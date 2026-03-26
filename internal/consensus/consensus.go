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
}

func New(id string, peers []string, f int, wal *storage.WAL, priv []byte, pubs map[string][]byte, net *network.Client) *Consensus {
	weights := make(map[string]int)
	weights[id] = 1
	for _, p := range peers {
		weights[p] = 1
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
		timer:           time.NewTimer(5 * time.Second),
	}
}

func (c *Consensus) GetLeader() string {
	allNodes := append([]string{c.ID}, c.Peers...)
	// In a real system, we'd sort these or use a consistent order
	// For now, let's just use the View to pick a node.
	// To make it weighted, we'd sum weights and pick based on cumulative weight.
	
	totalWeight := 0
	for _, w := range c.Weights {
		totalWeight += w
	}
	
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
	return c.GetLeader() == c.ID
}

func (c *Consensus) Start(digest [64]byte) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.IsLeader() {
		fmt.Printf("⚠️ Node %s is not the leader for View %d. Ignoring Start request.\n", c.ID, c.View)
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
	c.Network.Broadcast(c.Peers, msg)
}

func (c *Consensus) sign(msg *types.ConsensusMessage) {
	msg.Signature = nil
	data, _ := proto.Marshal(msg)
	msg.Signature = crypto.Sign(c.PrivateKey, data)
}

func (c *Consensus) Handle(msg *types.ConsensusMessage) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 🛡️ Byzantine Validation: Verify sender identity and signature
	if pub, ok := c.PublicKeys[msg.NodeId]; ok {
		// 🏁 v1.2: Verify the entire message content
		sig := msg.Signature
		msg.Signature = nil
		data, _ := proto.Marshal(msg)
		msg.Signature = sig

		if !crypto.Verify(pub, data, sig) {
			fmt.Printf("⛔ Node %s: SIGNATURE VERIFICATION FAILED from Node %s. Dropping message.\n", c.ID, msg.NodeId)
			return
		}
	} else {
		fmt.Printf("⚠️ Node %s: UNKNOWN NODE ID %s. Dropping message.\n", c.ID, msg.NodeId)
		return
	}

	// Only process messages for the current view
	if msg.View < c.View {
		return
	}

	// 🛡️ Digest Validation: Only for standard consensus phases
	if msg.Type == types.MessageType_PREPREPARE || msg.Type == types.MessageType_PREPARE || msg.Type == types.MessageType_COMMIT {
		if len(msg.Digest) != 64 {
			fmt.Printf("❌ Node %s: Rejected message with invalid digest length (%d bytes)\n", c.ID, len(msg.Digest))
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
			c.Network.Broadcast(c.Peers, commit)
		}

	case types.MessageType_COMMIT:
		if _, ok := c.Commit[key]; !ok {
			c.Commit[key] = make(map[string]bool)
		}
		c.Commit[key][msg.NodeId] = true

		if len(c.Commit[key]) >= 2*c.F+1 {
			fmt.Println("✅ CONSENSUS REACHED [View:", c.View, "]:", key)
			c.WAL.Write("COMMITTED:" + key)
			c.resetTimer()
		}

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
	}
}

func (c *Consensus) resetTimer() {
	if !c.timer.Stop() {
		select {
		case <-c.timer.C:
		default:
		}
	}
	c.timer.Reset(5 * time.Second)
}

func (c *Consensus) Monitor() {
	for range c.timer.C {
		c.mu.Lock()
		fmt.Printf("⏰ Node %s detected Leader Timeout. Initiating View Change...\n", c.ID)
		nextView := c.View + 1
		msg := &types.ConsensusMessage{
			Type:   types.MessageType_VIEW_CHANGE,
			View:   nextView,
			NodeId: c.ID,
		}
		c.sign(msg)
		c.Network.Broadcast(c.Peers, msg)
		c.mu.Unlock()
		c.resetTimer()
	}
}
