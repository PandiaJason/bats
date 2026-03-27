package consensus

import (
	"encoding/hex"
	"fmt"
	"sync"

	"bats/internal/crypto"
	"bats/internal/network"
	"bats/internal/storage"
	"bats/internal/types"
	"google.golang.org/protobuf/proto"
	"sort"
	"time"
)

type Consensus struct {
	mu sync.Mutex

	View            uint64
	Prepare         map[string]map[string]bool
	Commit          map[string]map[string]bool
	ViewChangeVotes map[uint64]map[string]bool
	Weights         map[string]int

	F     int
	ID    string
	Peers []string
	WAL   *storage.WAL

	// 🔐 Security: Ed25519 keys for Byzantine-safe message authentication
	PrivateKey []byte
	PublicKeys map[string][]byte

	Network *network.Client
	timer   *time.Timer

	OnCommit func([64]byte)
}

func New(id string, peers []string, f int, wal *storage.WAL, priv []byte, pubs map[string][]byte, net *network.Client, onCommit func([64]byte)) *Consensus {
	weights := make(map[string]int)
	for peerID := range pubs {
		weights[peerID] = 1
	}

	return &Consensus{
		Prepare:         make(map[string]map[string]bool),
		Commit:          make(map[string]map[string]bool),
		ViewChangeVotes: make(map[uint64]map[string]bool),
		Weights:         weights,
		F:               f,
		ID:              id,
		Peers:           peers,
		WAL:             wal,
		PrivateKey:      priv,
		PublicKeys:      pubs,
		Network:         net,
		timer:           time.NewTimer(500 * time.Second),
		OnCommit:        onCommit,
	}
}

func (c *Consensus) RecalculateF() {
	// N = Len(PublicKeys)
	n := len(c.PublicKeys)
	c.F = (n - 1) / 3
}

func (c *Consensus) AddPeer(id string, pub []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
 
	if _, ok := c.PublicKeys[id]; ok {
		return
	}
 
	c.PublicKeys[id] = pub
	c.Weights[id] = 1
	if id != c.ID {
		c.Peers = append(c.Peers, id)
	}
	c.RecalculateF()
	fmt.Printf("🛡️ Node %s: Membership Updated. Total Nodes:%d, F:%d\n", c.ID, len(c.PublicKeys), c.F)
}

func (c *Consensus) GetPeers() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	p := make([]string, len(c.Peers))
	copy(p, c.Peers)
	return p
}

func (c *Consensus) GetLeader() string {
	// 🔑 Leadership determinism: Sort all known nodes
	var allNodes []string
	totalWeight := 0
	for id, w := range c.Weights {
		allNodes = append(allNodes, id)
		totalWeight += w
	}
	// Sort to ensure all nodes pick the same leader for the same view
	sort.Strings(allNodes)
	
	target := int(c.View) % totalWeight
	current := 0
	for i := 0; i < len(allNodes); i++ {
		current += c.Weights[allNodes[i]]
		if current > target {
			return allNodes[i]
		}
	}
	return allNodes[0]
}

func (c *Consensus) IsLeader() bool {
	leader := c.GetLeader()
	// DEBUG
	fmt.Printf("[DEBUG] IsLeader check: c.ID=%s, c.GetLeader()=%s\n", c.ID, leader)
	return leader == c.ID
}

func (c *Consensus) Start(digest [64]byte) {
	c.mu.Lock()
	// defer c.mu.Unlock() // Removed defer

	if !c.IsLeader() {
		fmt.Printf("⚠️ Node %s is not the leader for View %d. Ignoring Start request.\n", c.ID, c.View)
		c.mu.Unlock() // Explicit unlock
		return
	}

	msg := &types.ConsensusMessage{
		Type:   types.MessageType_PREPREPARE,
		View:   c.View,
		Digest: digest[:],
		NodeId: c.ID,
	}

	// 🔐 Digital Signature for authenticating the PrePrepare message
	c.sign(msg)
	peers := c.GetPeers()
	c.mu.Unlock() // Unlock before broadcasting/local handle
	
	c.Network.Broadcast(peers, msg)
	
	// 🏠 Local processing: Leader also "handles" its own PrePrepare
	c.Handle(msg)
}

func (c *Consensus) Heartbeat() {
	c.mu.Lock()
	if !c.IsLeader() {
		c.mu.Unlock()
		return
	}
	msg := &types.ConsensusMessage{
		Type:   types.MessageType_PREPREPARE,
		View:   c.View,
		Digest: make([]byte, 64), // Heartbeat signature
		NodeId: c.ID,
	}
	c.sign(msg)
	peers := c.GetPeers()
	c.mu.Unlock()
	c.Network.Broadcast(peers, msg)
}

func (c *Consensus) sign(msg *types.ConsensusMessage) {
	msg.Signature = nil
	data, _ := proto.Marshal(msg)
	msg.Signature = crypto.Sign(c.PrivateKey, data)
}

func (c *Consensus) Handle(msg *types.ConsensusMessage) {
	c.mu.Lock()
	// defer c.mu.Unlock() // Removed defer

	// 🛡️ Byzantine Validation: Verify sender identity and signature
	if pub, ok := c.PublicKeys[msg.NodeId]; ok {
		// 🏁 v1.2: Verify the entire message content
		sig := msg.Signature
		msg.Signature = nil
		data, _ := proto.Marshal(msg)
		msg.Signature = sig

		if !crypto.Verify(pub, data, sig) {
			fmt.Printf("⛔ Node %s: SIGNATURE VERIFICATION FAILED from Node %s. Dropping message.\n", c.ID, msg.NodeId)
			c.mu.Unlock() // Explicit unlock
			return
		}
	} else {
		fmt.Printf("⚠️ Node %s: UNKNOWN NODE ID %s. Dropping message.\n", c.ID, msg.NodeId)
		c.mu.Unlock() // Explicit unlock
		return
	}

	// Only process messages for the current view
	if msg.View < c.View {
		c.mu.Unlock() // Explicit unlock
		return
	}

	// 🛡️ Digest Validation: Only for standard consensus phases
	if msg.Type == types.MessageType_PREPREPARE || msg.Type == types.MessageType_PREPARE || msg.Type == types.MessageType_COMMIT {
		if len(msg.Digest) != 64 {
			fmt.Printf("❌ Node %s: Rejected message with invalid digest length (%d bytes)\n", c.ID, len(msg.Digest))
			c.mu.Unlock() // Explicit unlock
			return
		}
	}

	key := hex.EncodeToString(msg.Digest)

	switch msg.Type {

	case types.MessageType_PREPREPARE:
		reply := &types.ConsensusMessage{
			Type:   types.MessageType_PREPARE,
			View:   c.View,
			Digest: msg.Digest,
			NodeId: c.ID,
		}
		c.sign(reply)
		c.mu.Unlock() // Explicit unlock
		c.Network.Broadcast(c.Peers, reply)

	case types.MessageType_PREPARE:
		if _, ok := c.Prepare[key]; !ok {
			c.Prepare[key] = make(map[string]bool)
		}
		c.Prepare[key][msg.NodeId] = true

		if len(c.Prepare[key]) >= 2*c.F+1 {
			commit := &types.ConsensusMessage{
				Type:   types.MessageType_COMMIT,
				View:   c.View,
				Digest: msg.Digest,
				NodeId: c.ID,
			}
			c.sign(commit)
			peers := c.GetPeers()
			c.mu.Unlock() // Explicit unlock
			c.Network.Broadcast(peers, commit)
			return // Return after broadcasting commit and unlocking
		}
		c.mu.Unlock() // Explicit unlock if not broadcasting commit

	case types.MessageType_COMMIT:
		if _, ok := c.Commit[key]; !ok {
			c.Commit[key] = make(map[string]bool)
		}
		c.Commit[key][msg.NodeId] = true

		if len(c.Commit[key]) >= 2*c.F+1 {
			fmt.Printf("✅ CONSENSUS REACHED [View:%d]: %s\n", c.View, key)
			c.WAL.Write("COMMITTED:" + key)
			c.resetTimer()

			if c.OnCommit != nil {
				var d [64]byte
				copy(d[:], msg.Digest)
				c.mu.Unlock() // Explicit unlock before calling OnCommit
				c.OnCommit(d)
				return // Return after OnCommit
			}
		}
		c.mu.Unlock() // Explicit unlock if not calling OnCommit

	case types.MessageType_VIEW_CHANGE:
		targetView := msg.View
		if _, ok := c.ViewChangeVotes[targetView]; !ok {
			c.ViewChangeVotes[targetView] = make(map[string]bool)
		}
		c.ViewChangeVotes[targetView][msg.NodeId] = true

		if len(c.ViewChangeVotes[targetView]) >= 2*c.F+1 && targetView > c.View {
			fmt.Printf("🔄 VIEW CHANGE QUORUM REACHED for View %d. Transitioning...\n", targetView)
			c.View = targetView
			c.Prepare = make(map[string]map[string]bool)
			c.Commit = make(map[string]map[string]bool)
			c.resetTimer()

			if c.IsLeader() {
				fmt.Printf("👑 Node %s is the NEW LEADER for View %d.\n", c.ID, c.View)
			}
		}
		c.mu.Unlock() // Explicit unlock at the end of the VIEW_CHANGE case
	}
}

func (c *Consensus) resetTimer() {
	if !c.timer.Stop() {
		select {
		case <-c.timer.C:
		default:
		}
	}
	c.timer.Reset(500 * time.Second)
}

func (c *Consensus) Monitor() {
	for range c.timer.C {
		c.mu.Lock() // Lock for the entire block
		if c.IsLeader() {
			c.mu.Unlock() // Unlock before calling Heartbeat, as Heartbeat handles its own locking
			c.Heartbeat()
		} else {
			// Trigger View Change if no progress
			fmt.Printf("⏰ Node %s detected Leader Timeout. Initiating View Change...\n", c.ID)
			nextView := c.View + 1
			msg := &types.ConsensusMessage{
				Type:   types.MessageType_VIEW_CHANGE,
				View:   nextView,
				NodeId: c.ID,
			}
			c.sign(msg)
			peers := c.GetPeers()
			c.Network.Broadcast(peers, msg)
			c.mu.Unlock() // Unlock after view change logic
		}
		c.resetTimer() // Reset timer regardless of leader status or view change
	}
}
