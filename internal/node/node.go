package node

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"bats/internal/ai"
	"bats/internal/consensus"
	"bats/internal/crypto"
	"bats/internal/network"
	"bats/internal/types"
	"bats/internal/wal"

	"google.golang.org/protobuf/proto"
)

// ConsensusTimeout is the maximum time HandleValidate will block waiting
// for PBFT quorum on a state-mutating action. Default: 800ms.
// Override with BATS_CONSENSUS_TIMEOUT_MS environment variable.
var ConsensusTimeout = 800 * time.Millisecond

func init() {
	if v := os.Getenv("BATS_CONSENSUS_TIMEOUT_MS"); v != "" {
		if ms, err := strconv.Atoi(v); err == nil {
			ConsensusTimeout = time.Duration(ms) * time.Millisecond
		}
	}
}

// Node represents a single BATS cluster participant.
// It owns the consensus engine, WAL, AI safety gate, and HTTP server.
type Node struct {
	ID        string
	Port      string
	Peers     []string
	Consensus *consensus.Consensus
	Network   *network.Client
	WAL       *wal.WAL
	AI        ai.Provider
	pending   map[[64]byte]chan bool
	pendingMu sync.Mutex
	mu        sync.Mutex

	// SeenNonces prevents replay attacks by tracking used nonces.
	// Keys are nonce strings, values are the unix timestamp they arrived.
	SeenNonces sync.Map
}

func NewNode(id string, port string, peers []string) *Node {
	log, _ := wal.NewWAL(id)

	// Load Ed25519 identity for this node
	priv, _ := os.ReadFile("certs/" + id + ".identity")
	pub, _ := os.ReadFile("certs/" + id + ".pub")

	peerPubs := make(map[string][]byte)
	peerPubs[id] = pub

	for _, p := range peers {
		// Derive peer ID from address suffix (e.g. "localhost:8001" -> "node1")
		pID := "node" + p[len(p)-1:]
		peerPub, _ := os.ReadFile("certs/" + pID + ".pub")
		peerPubs[pID] = peerPub
	}

	netClient := network.NewClient(id)

	f := (len(peerPubs) - 1) / 3
	if f == 0 {
		f = 1
	}

	n := &Node{
		ID:      id,
		Port:    port,
		Peers:   peers,
		Network: netClient,
		WAL:     log,
		AI:      ai.GetProvider(os.Getenv(strings.ToUpper(id) + "_AI_PROVIDER")),
		pending: make(map[[64]byte]chan bool),
	}

	n.Consensus = consensus.New(id, peers, f, log, priv, peerPubs, netClient, n.onCommit)
	go n.Consensus.Monitor()
	return n
}

// onCommit is called by the consensus engine when a digest reaches 2f+1 commits.
// It unblocks the synchronous wait in HandleValidate for write operations.
func (n *Node) onCommit(digest [64]byte) {
	n.pendingMu.Lock()
	defer n.pendingMu.Unlock()
	if ch, ok := n.pending[digest]; ok {
		ch <- true
		delete(n.pending, digest)
	}
}

func (n *Node) HandleConsensus(w http.ResponseWriter, r *http.Request) {
	var msg types.ConsensusMessage
	data, err := io.ReadAll(r.Body)
	if err != nil {
		return
	}
	if err := proto.Unmarshal(data, &msg); err != nil {
		http.Error(w, "Invalid message", http.StatusBadRequest)
		return
	}
	n.Consensus.Handle(&msg)
	w.WriteHeader(http.StatusOK)
}

func (n *Node) StatusHandler(w http.ResponseWriter, r *http.Request) {
	status := &types.NodeStatus{
		Id:       n.ID,
		Alive:    true,
		View:     n.Consensus.View,
		IsLeader: n.Consensus.IsLeader(),
	}
	data, _ := proto.Marshal(status)
	w.Header().Set("Content-Type", "application/x-protobuf")
	w.Write(data)
}

func (n *Node) HandleAITask(w http.ResponseWriter, r *http.Request) {
	if !n.Consensus.IsLeader() {
		n.forward(w, r)
		return
	}
	prompt := r.URL.Query().Get("prompt")
	if prompt == "" {
		http.Error(w, "Missing prompt", http.StatusBadRequest)
		return
	}

	fmt.Printf("[BATS] Node %s: Querying %s for Multi-Model Consensus...\n", n.ID, n.AI.Name())
	result, err := n.AI.Query(prompt)
	if err != nil {
		http.Error(w, "AI Query failed", http.StatusInternalServerError)
		return
	}

	digest := crypto.Digest(result)
	n.Consensus.Start(digest)
	fmt.Fprintf(w, "Task submitted. Result: %s\n", result)
}

func (n *Node) HandleJoin(w http.ResponseWriter, r *http.Request) {
	var req types.MembershipJoinRequest
	data, _ := io.ReadAll(r.Body)
	if err := proto.Unmarshal(data, &req); err != nil {
		http.Error(w, "Invalid proto", http.StatusBadRequest)
		return
	}

	fmt.Printf("[BATS] Node %s: Received Join Request from %s (%s)\n", n.ID, req.Id, req.Port)
	n.Consensus.AddPeer(req.Id, req.PublicKey)

	update := &types.ClusterUpdate{
		NewNode: &types.NodeStatus{Id: req.Id, Port: req.Port, Alive: true},
	}
	updateData, _ := proto.Marshal(update)

	peers := n.Consensus.GetPeers()
	for _, p := range peers {
		if p == req.Port || strings.Contains(p, req.Port) {
			continue
		}
		go func(addr string) {
			client := n.Network.GetHTTPClient()
			client.Post("https://"+addr+"/cluster-update", "application/x-protobuf", bytes.NewBuffer(updateData))
		}(p)
	}

	resp := &types.MembershipJoinResponse{
		Approved:    true,
		CurrentView: n.Consensus.View,
		F:           uint32(n.Consensus.F),
	}
	for id := range n.Consensus.PublicKeys {
		resp.Nodes = append(resp.Nodes, &types.NodeStatus{Id: id})
	}

	respData, _ := proto.Marshal(resp)
	w.WriteHeader(http.StatusOK)
	w.Write(respData)
}

func (n *Node) HandleClusterUpdate(w http.ResponseWriter, r *http.Request) {
	var update types.ClusterUpdate
	data, _ := io.ReadAll(r.Body)
	if err := proto.Unmarshal(data, &update); err != nil {
		return
	}
	fmt.Printf("[BATS] Node %s: Cluster update. Adding Node %s\n", n.ID, update.NewNode.Id)
	pub, _ := os.ReadFile("certs/" + update.NewNode.Id + ".pub")
	n.Consensus.AddPeer(update.NewNode.Id, pub)
	w.WriteHeader(http.StatusOK)
}

// HandleValidate is the core safety pipeline. It implements a two-stage gate:
//
//  1. AI Heuristic Gate: Classifies the action with a confidence score.
//     If the action is UNSAFE, it is blocked immediately.
//
//  2. Consensus Gate: Determines whether to use the fast-path or sync-path.
//     SAFE_READ actions with confidence >= 0.95 are approved optimistically
//     in under 100ms, with PBFT running asynchronously in the background
//     for audit trail consistency.
//     All other SAFE actions go through synchronous PBFT (blocks until 2f+1).
func (n *Node) HandleValidate(w http.ResponseWriter, r *http.Request) {
	if !n.Consensus.IsLeader() {
		n.forward(w, r)
		return
	}

	var req struct {
		Action string `json:"action"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	// --- Stage 1: AI Safety Evaluation ---
	verdict := n.AI.Evaluate(req.Action)
	fmt.Printf("[BATS] Node %s: Safety verdict for [%s]: %s (confidence=%.2f)\n",
		n.ID, req.Action, verdict.Classification, verdict.Confidence)

	if verdict.Classification == "UNSAFE" {
		fmt.Printf("[BATS-BLOCKED] Node %s: %s\n", n.ID, verdict.Reason)

		// Log the blocked action to the tamper-evident WAL
		n.WAL.Append(
			fmt.Sprintf("%x", crypto.Digest(req.Action)),
			"external-agent",
			"BLOCKED",
			nil,
		)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"approved":   false,
			"reason":     verdict.Reason,
			"confidence": verdict.Confidence,
		})
		return
	}

	digest := crypto.Digest(req.Action)

	// --- Stage 2: Consensus Path Selection ---

	if verdict.IsFastPathEligible() {
		// FAST-PATH: Confidence >= 0.95 for a non-mutating read.
		// Response goes out FIRST. All I/O (WAL, logging, PBFT) is deferred
		// to a background goroutine to keep p95 latency under 2ms.
		digestHex := fmt.Sprintf("%x", digest)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"approved":   true,
			"digest":     digestHex,
			"fast_path":  true,
			"confidence": verdict.Confidence,
		})

		// All post-response work: WAL persistence, audit log, background PBFT.
		// None of this blocks the client.
		go func() {
			n.WAL.Append(digestHex, "external-agent", "APPROVED_FAST_PATH", nil)
			n.Consensus.Start(digest)
		}()
		return
	}

	// SYNC-PATH: State-mutating action. Block until PBFT reaches 2f+1 commits.
	fmt.Printf("[BATS-SYNC] Node %s: confidence=%.2f, initiating synchronous PBFT\n",
		n.ID, verdict.Confidence)

	ch := make(chan bool, 1)
	n.pendingMu.Lock()
	n.pending[digest] = ch
	n.pendingMu.Unlock()

	n.Consensus.Start(digest)

	select {
	case <-ch:
		n.WAL.Append(
			fmt.Sprintf("%x", digest),
			"external-agent",
			"COMMITTED",
			nil,
		)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"approved":   true,
			"digest":     fmt.Sprintf("%x", digest),
			"fast_path":  false,
			"confidence": verdict.Confidence,
		})

	case <-time.After(ConsensusTimeout):
		n.pendingMu.Lock()
		delete(n.pending, digest)
		n.pendingMu.Unlock()

		n.WAL.Append(
			fmt.Sprintf("%x", digest),
			"external-agent",
			"TIMEOUT",
			nil,
		)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"approved": false,
			"reason":   fmt.Sprintf("consensus timeout after %v", ConsensusTimeout),
		})
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
		w.Header().Set("Content-Disposition", "attachment; filename=bats_audit.csv")
		n.WAL.ExportCSV(w)
	default:
		w.Header().Set("Content-Type", "application/json")
		n.WAL.ExportJSON(w)
	}
}

// requireSecurityHeaders is middleware that enforces replay attack prevention.
// Every protected request must carry:
//   - X-BATS-Nonce: a unique, never-reused string
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
			fmt.Printf("[BATS-SECURITY] Node %s: Replay attack blocked. Nonce: %s\n", n.ID, nonce)
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

	fmt.Printf("[BATS-CORE] Node %-6s | PORT: %-5s | TIMEOUT: %v | STATUS: ACTIVE (v3.1)\n", n.ID, port, ConsensusTimeout)
	http.ListenAndServeTLS(":"+port, certFile, keyFile, mux)
}

func (n *Node) forward(w http.ResponseWriter, r *http.Request) {
	var allNodes []string
	for id := range n.Consensus.PublicKeys {
		allNodes = append(allNodes, id)
	}
	sort.Strings(allNodes)
	leaderID := allNodes[int(n.Consensus.View%uint64(len(allNodes)))]

	ports := map[string]string{
		"node1": "8001", "node2": "8002", "node3": "8003",
		"node4": "8004", "node5": "8005",
	}
	leaderAddr := "localhost:" + ports[leaderID]

	url := fmt.Sprintf("https://%s%s", leaderAddr, r.URL.Path)
	if r.URL.RawQuery != "" {
		url += "?" + r.URL.RawQuery
	}

	body, _ := io.ReadAll(r.Body)
	req, _ := http.NewRequest(r.Method, url, bytes.NewBuffer(body))
	req.Header = r.Header

	resp, err := n.Network.GetHTTPClient().Do(req)
	if err != nil {
		http.Error(w, "Forwarding failed", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	io.Copy(w, resp.Body)
}
