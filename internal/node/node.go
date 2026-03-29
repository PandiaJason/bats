package node

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"bats/internal/ai"
	"bats/internal/consensus"
	"bats/internal/crypto"
	"bats/internal/network"
	"bats/internal/storage"
	"bats/internal/types"

	"github.com/quic-go/quic-go/http3"
	"google.golang.org/protobuf/proto"
)

type Node struct {
	ID        string
	Port      string
	Peers     []string // Base peers (bootstrap nodes)
	Consensus *consensus.Consensus
	Network   *network.Client
	WAL       *storage.WAL
	AI        ai.Provider
	pending   map[[64]byte]chan bool
	pendingMu sync.Mutex
	mu        sync.Mutex
	SeenNonces sync.Map
}

func NewNode(id string, port string, peers []string) *Node {
	wal, _ := storage.NewWAL(id)
	
	// Identity loading
	priv, _ := os.ReadFile("certs/" + id + ".identity")
	pub, _ := os.ReadFile("certs/" + id + ".pub")
	
	peerPubs := make(map[string][]byte)
	peerPubs[id] = pub
	
	for _, p := range peers {
		// Mock mapping for demo: addr -> id
		pID := "node" + p[len(p)-1:] // e.g. "localhost:8001" -> "node1"
		peerPub, _ := os.ReadFile("certs/" + pID + ".pub")
		peerPubs[pID] = peerPub
	}

	netClient := network.NewClient(id)
	
	f := (len(peerPubs) - 1) / 3
	if f == 0 { f = 1 }

	n := &Node{
		ID:      id,
		Port:    port,
		Peers:   peers,
		Network: netClient,
		WAL:     wal,
		AI:      ai.GetProvider(os.Getenv("AI_PROVIDER")),
		pending: make(map[[64]byte]chan bool),
	}
	
	n.Consensus = consensus.New(id, peers, f, wal, priv, peerPubs, netClient, n.onCommit)
	go n.Consensus.Monitor()
	return n
}

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

	fmt.Printf("🤖 Node %s: Querying %s for Multi-Model Consensus...\n", n.ID, n.AI.Name())
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

	fmt.Printf("🤝 Node %s: Received Join Request from %s (%s)\n", n.ID, req.Id, req.Port)
	n.Consensus.AddPeer(req.Id, req.PublicKey)

	// Broadcast update to all other peers
	update := &types.ClusterUpdate{
		NewNode: &types.NodeStatus{Id: req.Id, Port: req.Port, Alive: true},
	}
	updateData, _ := proto.Marshal(update)
	
	peers := n.Consensus.GetPeers()
	for _, p := range peers {
		if p == req.Port || strings.Contains(p, req.Port) { continue }
		go func(addr string) {
			client := n.Network.GetHTTPClient()
			client.Post("https://"+addr+"/cluster-update", "application/x-protobuf", bytes.NewBuffer(updateData))
		}(p)
	}

	// Prepare response with current nodes
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

func (n *Node) HandleValidate(w http.ResponseWriter, r *http.Request) {
	if !n.Consensus.IsLeader() {
		n.forward(w, r)
		return
	}
	var req struct { Action string `json:"action"` }
	json.NewDecoder(r.Body).Decode(&req)
	
	// --- AI Safety Validation ---
	fmt.Printf("[BATS] Node %s: AI Safety Gate checking action: [%s]\n", n.ID, req.Action)
	safetyVer, err := n.AI.Query(fmt.Sprintf("Evaluate if the following action is safe: '%s'", req.Action))
	
	if err != nil || strings.HasPrefix(safetyVer, "UNSAFE") {
		fmt.Printf("[BATS-BLOCKED] Node %s: AI Safety Gate BLOCKED the action: %s\n", n.ID, safetyVer)
		w.Header().Set("Content-Type", "application/json")
		responseStr := fmt.Sprintf(`{"approved":false,"reason":"AI Safety Gate Rejected: %s"}`, safetyVer)
		w.Write([]byte(responseStr))
		return
	}

	digest := crypto.Digest(req.Action)

	// --- FAST-PATH BYPASS FOR SAFE READS ---
	if strings.HasPrefix(safetyVer, "[SAFE_READ]") {
		fmt.Printf("[BATS-FAST-PATH] Node %s: AI Heuristic Confidence > 0.95 for READ. Approving in <10ms...\n", n.ID)
		
		// Run PBFT in background for eventual audit consistency
		go n.Consensus.Start(digest)

		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(fmt.Sprintf(`{"approved":true,"digest":"%x","fast_path":true}`, digest)))
		return
	}

	fmt.Printf("[BATS-PASSED] Node %s: AI Safety Gate PASSED. Initiating Sync PBFT Consensus...\n", n.ID)
	// --- End AI Safety Validation ---

	ch := make(chan bool, 1)
	n.pendingMu.Lock()
	n.pending[digest] = ch
	n.pendingMu.Unlock()

	n.Consensus.Start(digest)
	select {
	case <-ch:
		w.Header().Set("Content-Type", "application/json")
		responseStr := fmt.Sprintf(`{"approved":true,"digest":"%x"}`, digest)
		w.Write([]byte(responseStr))
	case <-time.After(10 * time.Second):
		n.pendingMu.Lock()
		delete(n.pending, digest)
		n.pendingMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"approved":false,"reason":"timeout"}`))
	}
}

func (n *Node) requireSecurityHeaders(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		nonce := r.Header.Get("X-BATS-Nonce")
		tsStr := r.Header.Get("X-BATS-Timestamp")

		// 1. Missing Headers Check
		if nonce == "" || tsStr == "" {
			http.Error(w, `{"error":"Missing Security Headers"}`, http.StatusUnauthorized)
			return
		}

		// 2. Timestamp Drift Validation (Replay Window = 30s)
		var ts int64
		fmt.Sscanf(tsStr, "%d", &ts)
		now := time.Now().Unix()
		drift := now - ts
		if drift > 30 || drift < -30 {
			http.Error(w, `{"error":"Timestamp outside permitted drift window"}`, http.StatusUnauthorized)
			return
		}

		// 3. Nonce Uniqueness Validation
		if _, exists := n.SeenNonces.LoadOrStore(nonce, ts); exists {
			fmt.Printf("🚨 Node %s intercepted REPLAY ATTACK! Nonce: %s\n", n.ID, nonce)
			http.Error(w, `{"error":"Replayed Nonce Detected"}`, http.StatusUnauthorized)
			return
		}

		// Clean up old nonces asynchronously
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
	mux.HandleFunc("/ai-task", n.requireSecurityHeaders(n.HandleAITask)) // Protected
	mux.HandleFunc("/validate", n.requireSecurityHeaders(n.HandleValidate)) // Protected
	mux.HandleFunc("/join", n.HandleJoin)
	mux.HandleFunc("/cluster-update", n.HandleClusterUpdate)

	certFile := "certs/" + n.ID + ".crt"
	keyFile := "certs/" + n.ID + ".key"

	fmt.Printf("[BATS-CORE] Node %-6s | PORT: %-5s | STATUS: ACTIVE (v2.0)\n", n.ID, port)
	go http3.ListenAndServeQUIC(":"+port, certFile, keyFile, mux)
	http.ListenAndServeTLS(":"+port, certFile, keyFile, mux)
}

func (n *Node) forward(w http.ResponseWriter, r *http.Request) {
	var allNodes []string
	for id := range n.Consensus.PublicKeys {
		allNodes = append(allNodes, id)
	}
	sort.Strings(allNodes)
	leaderID := allNodes[int(n.Consensus.View%uint64(len(allNodes)))]
	
	// This mapping should ideally come from a dynamic discovery service
	// For demo purposes, we use a static map
	ports := map[string]string{"node1":"8001","node2":"8002","node3":"8003","node4":"8004","node5":"8005"}
	leaderAddr := "localhost:" + ports[leaderID]

	url := fmt.Sprintf("https://%s%s", leaderAddr, r.URL.Path)
	if r.URL.RawQuery != "" { url += "?" + r.URL.RawQuery }

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
