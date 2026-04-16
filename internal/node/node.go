package node

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"bats/internal/ai"
	"bats/internal/crypto"
	"bats/internal/network"
	"bats/internal/policy"
	"bats/internal/types"
	"bats/internal/wal"

	"google.golang.org/protobuf/proto"
)

const (
	// MaxBodySize is the maximum allowed request body size (1MB).
	MaxBodySize = 1 << 20

	// NonceWindow is the time window for nonce validity.
	NonceWindow = 30 * time.Second

	// NonceCleanupInterval is how often expired nonces are purged.
	NonceCleanupInterval = 30 * time.Second

	// TimestampDrift is the maximum allowed clock drift.
	TimestampDrift = 30

	// RateLimitRequests is the max requests per rate limit window.
	RateLimitRequests = 100

	// RateLimitWindow is the rate limit window duration.
	RateLimitWindow = 10 * time.Second
)

// Node represents a single WAND enforcement layer node.
// It owns the deterministic policy engine, WAL, optional AI annotator, and HTTP server.
type Node struct {
	ID      string
	Port    string
	Peers   []string
	Network *network.Client
	WAL     *wal.WAL
	AI      ai.Provider
	Logger  *slog.Logger

	// SeenNonces prevents replay attacks by tracking used nonces.
	SeenNonces sync.Map

	// rateLimiter tracks per-IP request counts.
	rateLimiter sync.Map

	// cancel is used for graceful shutdown.
	cancel context.CancelFunc
}

func NewNode(id string, port string, peers []string) *Node {
	// Determine WAL path
	walDir := os.Getenv("WAND_DATA_DIR")
	if walDir == "" {
		walDir = "."
	}
	walPath := walDir + "/wal_" + id + ".log"

	walLog, err := wal.NewWAL(walPath)
	if err != nil {
		slog.Error("Failed to initialize WAL", "error", err, "path", walPath)
		os.Exit(1)
	}

	netClient := network.NewClient(id)

	providerStr := os.Getenv("NODE_LLM")
	if providerStr == "" {
		switch id {
		case "node1":
			providerStr = "anthropic"
		case "node2":
			providerStr = "openai"
		case "node3":
			providerStr = "google"
		default:
			providerStr = "local"
		}
	}

	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})).With("node", id)

	n := &Node{
		ID:      id,
		Port:    port,
		Peers:   peers,
		Network: netClient,
		WAL:     walLog,
		AI:      ai.GetProvider(providerStr),
		Logger:  logger,
	}

	return n
}

// Legacy cluster endpoints — kept as no-op stubs for backward compatibility.
func (n *Node) HandleConsensus(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func (n *Node) StatusHandler(w http.ResponseWriter, r *http.Request) {
	status := &types.NodeStatus{
		Id:       n.ID,
		Alive:    true,
		View:     1,
		IsLeader: false,
	}
	data, err := proto.Marshal(status)
	if err != nil {
		n.Logger.Error("Failed to marshal status", "error", err)
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/x-protobuf")
	w.Write(data)
}

func (n *Node) HandleJoin(w http.ResponseWriter, r *http.Request) {
	resp := &types.MembershipJoinResponse{
		Approved:    true,
		CurrentView: 1,
		F:           1,
	}
	respData, err := proto.Marshal(resp)
	if err != nil {
		n.Logger.Error("Failed to marshal join response", "error", err)
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write(respData)
}

// HandleValidate is the core WAND safety pipeline.
// Evaluates actions strictly via the deterministic policy engine.
//
// Three possible outcomes:
//   - BLOCK:     Immediately denied. Logged to WAL synchronously.
//   - CHALLENGE: Risky action flagged. Agent must ask the user to re-approve.
//   - ALLOW:     No dangerous pattern matched. Approved and logged.
//
// WAL writes happen BEFORE the HTTP response is sent to guarantee
// audit trail integrity even on crash.
func (n *Node) HandleValidate(w http.ResponseWriter, r *http.Request) {
	// Enforce request body size limit
	r.Body = http.MaxBytesReader(w, r.Body, MaxBodySize)

	var req struct {
		Action string `json:"action"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		n.Logger.Warn("Invalid request body", "error", err)
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	if req.Action == "" {
		http.Error(w, `{"error":"action field is required"}`, http.StatusBadRequest)
		return
	}

	// Single deterministic safety evaluation — sub-millisecond, no AI
	verdict := policy.Evaluate(req.Action)
	digest := crypto.Digest(req.Action)
	digestHex := fmt.Sprintf("%x", digest)

	w.Header().Set("Content-Type", "application/json")

	switch verdict.Decision {

	case "BLOCK":
		n.Logger.Info("Action BLOCKED",
			"action", req.Action,
			"reason", verdict.Reason,
			"category", verdict.Category,
		)

		// Synchronous WAL write — audit MUST complete before response
		if err := n.WAL.Append(digestHex, "external-agent", "BLOCKED", map[string]string{
			"reason":   verdict.Reason,
			"category": verdict.Category,
		}); err != nil {
			n.Logger.Error("WAL write failed for BLOCK", "error", err)
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"approved": false,
			"decision": "BLOCK",
			"reason":   verdict.Reason,
		})

	case "CHALLENGE":
		n.Logger.Info("Action CHALLENGED",
			"action", req.Action,
			"reason", verdict.Reason,
			"category", verdict.Category,
		)

		// Synchronous WAL write
		if err := n.WAL.Append(digestHex, "external-agent", "CHALLENGED", map[string]string{
			"reason":   verdict.Reason,
			"category": verdict.Category,
		}); err != nil {
			n.Logger.Error("WAL write failed for CHALLENGE", "error", err)
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"approved":  false,
			"decision":  "CHALLENGE",
			"reason":    verdict.Reason,
			"challenge": "This action is risky. The agent MUST ask the user for explicit re-approval before proceeding.",
		})

	default: // ALLOW
		n.Logger.Info("Action APPROVED",
			"action", req.Action,
			"digest", digestHex[:16],
		)

		// Synchronous WAL write — audit MUST complete before response
		if err := n.WAL.Append(digestHex, "external-agent", "APPROVED", nil); err != nil {
			n.Logger.Error("WAL write failed for APPROVE", "error", err)
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"approved": true,
			"decision": "ALLOW",
			"digest":   digestHex,
		})

		// Async: optional non-authoritative AI annotation (does NOT affect the decision)
		go func(action, hexHash string) {
			if n.AI == nil {
				return
			}
			meta, err := n.AI.Query("Provide brief metadata annotation for this action: " + action)
			if err != nil {
				n.Logger.Debug("AI annotation failed", "error", err)
				return
			}
			n.WAL.Append(hexHash, "ai-annotator", "ANNOTATION", map[string]string{
				"ai_metadata": meta,
			})
		}(req.Action, digestHex)
	}
}

// HandleAuditExport serves the hash-chained WAL as JSON for compliance.
func (n *Node) HandleAuditExport(w http.ResponseWriter, r *http.Request) {
	format := r.URL.Query().Get("format")
	if format == "" {
		format = "json"
	}

	switch format {
	case "csv":
		w.Header().Set("Content-Type", "text/csv")
		w.Header().Set("Content-Disposition", "attachment; filename=wand_audit.csv")
		if err := n.WAL.ExportCSV(w); err != nil {
			n.Logger.Error("CSV export failed", "error", err)
		}
	default:
		w.Header().Set("Content-Type", "application/json")
		if err := n.WAL.ExportJSON(w); err != nil {
			n.Logger.Error("JSON export failed", "error", err)
		}
	}
}

// requireSecurityHeaders is middleware that enforces replay attack prevention.
// Every protected request must carry:
//   - X-BATS-Nonce: a unique, never-reused string (header name kept for API compatibility)
//   - X-BATS-Timestamp: unix epoch seconds, must be within +/- 30s of server time
func (n *Node) requireSecurityHeaders(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		nonce := r.Header.Get("X-BATS-Nonce")
		tsStr := r.Header.Get("X-BATS-Timestamp")

		if nonce == "" || tsStr == "" {
			http.Error(w, `{"error":"Missing X-BATS-Nonce or X-BATS-Timestamp"}`, http.StatusUnauthorized)
			return
		}

		var ts int64
		fmt.Sscanf(tsStr, "%d", &ts)
		drift := time.Now().Unix() - ts
		if drift > int64(TimestampDrift) || drift < -int64(TimestampDrift) {
			http.Error(w, `{"error":"Timestamp drift exceeds 30s window"}`, http.StatusUnauthorized)
			return
		}

		if _, exists := n.SeenNonces.LoadOrStore(nonce, time.Now().Unix()); exists {
			n.Logger.Warn("Replay attack blocked", "nonce", nonce)
			http.Error(w, `{"error":"Replayed nonce detected"}`, http.StatusUnauthorized)
			return
		}

		next(w, r)
	}
}

// rateLimitMiddleware enforces per-IP rate limiting.
func (n *Node) rateLimitMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ip := r.RemoteAddr

		now := time.Now()
		val, _ := n.rateLimiter.LoadOrStore(ip, &rateBucket{
			count:   0,
			resetAt: now.Add(RateLimitWindow),
		})
		bucket := val.(*rateBucket)

		if now.After(bucket.resetAt) {
			bucket.count = 0
			bucket.resetAt = now.Add(RateLimitWindow)
		}

		bucket.count++
		if bucket.count > RateLimitRequests {
			n.Logger.Warn("Rate limit exceeded", "ip", ip)
			http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)
			return
		}

		next(w, r)
	}
}

type rateBucket struct {
	count   int
	resetAt time.Time
}

// startNonceCleanup runs a single background goroutine that periodically
// purges expired nonces. Replaces the per-request cleanup goroutine spam.
func (n *Node) startNonceCleanup(ctx context.Context) {
	ticker := time.NewTicker(NonceCleanupInterval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				cutoff := time.Now().Unix() - int64(NonceWindow.Seconds()*2)
				n.SeenNonces.Range(func(key, value interface{}) bool {
					if value.(int64) < cutoff {
						n.SeenNonces.Delete(key)
					}
					return true
				})
			}
		}
	}()
}

func (n *Node) Start(port string) {
	ctx, cancel := context.WithCancel(context.Background())
	n.cancel = cancel

	// Start background nonce cleanup
	n.startNonceCleanup(ctx)

	mux := http.NewServeMux()
	mux.HandleFunc("/consensus", n.HandleConsensus)
	mux.HandleFunc("/status", n.StatusHandler)
	mux.HandleFunc("/validate", n.rateLimitMiddleware(n.requireSecurityHeaders(n.HandleValidate)))
	mux.HandleFunc("/join", n.HandleJoin)
	mux.HandleFunc("/audit/export", n.HandleAuditExport)

	// Cert paths — configurable via env or default convention
	certDir := os.Getenv("WAND_CERT_DIR")
	if certDir == "" {
		certDir = "certs"
	}
	certFile := certDir + "/" + n.ID + ".crt"
	keyFile := certDir + "/" + n.ID + ".key"

	server := &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
		MaxHeaderBytes:    1 << 16, // 64KB
	}

	// Graceful shutdown on SIGTERM/SIGINT
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
		<-sigCh
		n.Logger.Info("Shutdown signal received, draining connections...")
		cancel()

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer shutdownCancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			n.Logger.Error("Shutdown error", "error", err)
		}
		n.Logger.Info("Server stopped gracefully")
	}()

	blockCount, challengeCount := policy.PatternCount()
	n.Logger.Info("WAND Enforcement Node starting",
		"port", port,
		"version", "5.0",
		"block_rules", blockCount,
		"challenge_rules", challengeCount,
	)

	// Keep legacy Printf for terminal visibility
	fmt.Printf("[WAND-CORE] Node %-6s | PORT: %-5s | STATUS: ENFORCER ACTIVE (v5.0)\n", n.ID, port)

	if err := server.ListenAndServeTLS(certFile, keyFile); err != nil && err != http.ErrServerClosed {
		n.Logger.Error("Server failed", "error", err)
		os.Exit(1)
	}
}
