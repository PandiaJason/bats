package node

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"bats/internal/ai"
	"bats/internal/crypto"
	"bats/internal/network"
	"bats/internal/policy"
	"bats/internal/types"
	"bats/internal/wal"

	"google.golang.org/protobuf/proto"
)



// Node represents a single WAND enforcement layer node.
// It owns the deterministic policy engine, WAL, optional AI annotator, and HTTP server.
type Node struct {
	ID        string
	Port      string
	Peers     []string
	Network   *network.Client
	WAL       *wal.WAL
	AI        ai.Provider
	mu        sync.Mutex

	// SeenNonces prevents replay attacks by tracking used nonces.
	// Keys are nonce strings, values are the unix timestamp they arrived.
	SeenNonces sync.Map
}

func NewNode(id string, port string, peers []string) *Node {
	log, _ := wal.NewWAL(id)
	
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
		case "node4":
			providerStr = "local"
		default:
			providerStr = "local"
		}
	}

	n := &Node{
		ID:      id,
		Port:    port,
		Peers:   peers,
		Network: netClient,
		WAL:     log,
		AI:      ai.GetProvider(providerStr),
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
	data, _ := proto.Marshal(status)
	w.Header().Set("Content-Type", "application/x-protobuf")
	w.Write(data)
}

func (n *Node) HandleAITask(w http.ResponseWriter, r *http.Request) {
    http.Error(w, "AI mult-model consensus removed in WAND", http.StatusNotImplemented)
}

func (n *Node) HandleJoin(w http.ResponseWriter, r *http.Request) {
	resp := &types.MembershipJoinResponse{
		Approved:    true,
		CurrentView: 1,
		F:           1,
	}
	respData, _ := proto.Marshal(resp)
	w.WriteHeader(http.StatusOK)
	w.Write(respData)
}

func (n *Node) HandleClusterUpdate(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}


// HandleValidate is the core WAND safety pipeline.
// Evaluates actions strictly via the deterministic policy engine.
//
// Three possible outcomes:
//   - BLOCK:     Immediately denied. Logged to WAL.
//   - CHALLENGE: Risky action flagged. Agent must ask the user to re-approve.
//   - ALLOW:     No dangerous pattern matched. Approved and logged.
func (n *Node) HandleValidate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Action string `json:"action"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	// Single deterministic safety evaluation — sub-millisecond, no AI
	verdict := policy.Evaluate(req.Action)
	digest := crypto.Digest(req.Action)
	digestHex := fmt.Sprintf("%x", digest)

	w.Header().Set("Content-Type", "application/json")

	switch verdict.Decision {

	case "BLOCK":
		fmt.Printf("[WAND-BLOCKED] Node %s: %s\n", n.ID, verdict.Reason)

		go func() {
			n.WAL.Append(digestHex, "external-agent", "BLOCKED", nil)
		}()

		json.NewEncoder(w).Encode(map[string]interface{}{
			"approved": false,
			"decision": "BLOCK",
			"reason":   verdict.Reason,
		})

	case "CHALLENGE":
		fmt.Printf("[WAND-CHALLENGE] Node %s: %s\n", n.ID, verdict.Reason)

		go func() {
			n.WAL.Append(digestHex, "external-agent", "CHALLENGED", nil)
		}()

		json.NewEncoder(w).Encode(map[string]interface{}{
			"approved":  false,
			"decision":  "CHALLENGE",
			"reason":    verdict.Reason,
			"challenge": "This action is risky. The agent MUST ask the user for explicit re-approval before proceeding.",
		})

	default: // ALLOW
		fmt.Printf("[WAND-APPROVED] Node %s: %s\n", n.ID, verdict.Reason)

		json.NewEncoder(w).Encode(map[string]interface{}{
			"approved": true,
			"decision": "ALLOW",
			"digest":   digestHex,
		})

		// Async WAL logging and optional non-authoritative AI annotation
		go func(action, hexHash string) {
			annotations := map[string]string{}
			if n.AI != nil {
				meta, err := n.AI.Query("Provide brief metadata annotation for this action: " + action)
				if err == nil {
					annotations["ai_metadata"] = meta
				}
			}
			n.WAL.Append(hexHash, "external-agent", "APPROVED", annotations)
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
		n.WAL.ExportCSV(w)
	default:
		w.Header().Set("Content-Type", "application/json")
		n.WAL.ExportJSON(w)
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
		if drift > 30 || drift < -30 {
			http.Error(w, `{"error":"Timestamp drift exceeds 30s window"}`, http.StatusUnauthorized)
			return
		}

		if _, exists := n.SeenNonces.LoadOrStore(nonce, ts); exists {
			fmt.Printf("[WAND-SECURITY] Node %s: Replay attack blocked. Nonce: %s\n", n.ID, nonce)
			http.Error(w, `{"error":"Replayed nonce detected"}`, http.StatusUnauthorized)
			return
		}

		// Async cleanup of nonces older than 60s
		go func() {
			n.SeenNonces.Range(func(key, value interface{}) bool {
				if time.Now().Unix()-value.(int64) > 60 {
					n.SeenNonces.Delete(key)
				}
				return true
			})
		}()

		next(w, r)
	}
}

func (n *Node) Start(port string) {
	mux := http.NewServeMux()
	mux.HandleFunc("/consensus", n.HandleConsensus)
	mux.HandleFunc("/status", n.StatusHandler)
	mux.HandleFunc("/ai-task", n.requireSecurityHeaders(n.HandleAITask))
	mux.HandleFunc("/validate", n.requireSecurityHeaders(n.HandleValidate))
	mux.HandleFunc("/join", n.HandleJoin)
	mux.HandleFunc("/cluster-update", n.HandleClusterUpdate)
	mux.HandleFunc("/audit/export", n.HandleAuditExport)

	certFile := "certs/" + n.ID + ".crt"
	keyFile := "certs/" + n.ID + ".key"

	fmt.Printf("[WAND-CORE] Node %-6s | PORT: %-5s | STATUS: ENFORCER ACTIVE (v4.0)\n", n.ID, port)
	http.ListenAndServeTLS(":"+port, certFile, keyFile, mux)
}


