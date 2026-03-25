package consensus

import (
	"encoding/hex"
	"fmt"
	"sync"

	"bats/internal/network"
	"bats/internal/storage"
	"bats/internal/types"
)

type Consensus struct {
	mu sync.Mutex

	View    uint64
	Prepare map[string]map[string]bool
	Commit  map[string]map[string]bool
	Weights map[string]int

	F     int
	ID    string
	Peers []string
	WAL   *storage.WAL
}

func New(id string, peers []string, f int, wal *storage.WAL) *Consensus {
	weights := make(map[string]int)
	// Default weights for all nodes (including peers and self)
	weights[id] = 1
	for _, p := range peers {
		weights[p] = 1
	}

	return &Consensus{
		Prepare: make(map[string]map[string]bool),
		Commit:  make(map[string]map[string]bool),
		Weights: weights,
		F:       f,
		ID:      id,
		Peers:   peers,
		WAL:     wal,
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

func (c *Consensus) Start(digest [32]byte) {
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
	network.Broadcast(c.Peers, msg)
}

func (c *Consensus) Handle(msg *types.ConsensusMessage) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Only process messages for the current view
	if msg.View < c.View {
		return
	}

	key := hex.EncodeToString(msg.Digest)

	switch msg.Type {

	case types.MessageType_PREPREPARE:
		network.Broadcast(c.Peers, &types.ConsensusMessage{
			Type:   types.MessageType_PREPARE,
			View:   c.View,
			Digest: msg.Digest,
			NodeId: c.ID,
		})

	case types.MessageType_PREPARE:
		if _, ok := c.Prepare[key]; !ok {
			c.Prepare[key] = make(map[string]bool)
		}
		c.Prepare[key][msg.NodeId] = true

		if len(c.Prepare[key]) >= 2*c.F+1 {
			network.Broadcast(c.Peers, &types.ConsensusMessage{
				Type:   types.MessageType_COMMIT,
				View:   c.View,
				Digest: msg.Digest,
				NodeId: c.ID,
			})
		}

	case types.MessageType_COMMIT:
		if _, ok := c.Commit[key]; !ok {
			c.Commit[key] = make(map[string]bool)
		}
		c.Commit[key][msg.NodeId] = true

		if len(c.Commit[key]) >= 2*c.F+1 {
			fmt.Println("✅ CONSENSUS REACHED [View:", c.View, "]:", key)
			c.WAL.Write("COMMITTED:" + key)
		}
	}
}
