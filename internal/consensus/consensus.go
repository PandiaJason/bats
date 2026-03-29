package consensus

import (
	"encoding/hex"
	"fmt"
	"sort"
	"sync"
	"time"

	"bats/internal/crypto"
	"bats/internal/network"
	"bats/internal/types"
	"bats/internal/wal"

	"google.golang.org/protobuf/proto"
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
	WAL   *wal.WAL

	PrivateKey []byte
	PublicKeys map[string][]byte

	Network *network.Client
	timer   *time.Timer

	OnCommit func([64]byte)
}

func New(id string, peers []string, f int, store *wal.WAL, priv []byte, pubs map[string][]byte, net *network.Client, onCommit func([64]byte)) *Consensus {
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
		WAL:             store,
		PrivateKey:      priv,
		PublicKeys:      pubs,
		Network:         net,
		timer:           time.NewTimer(500 * time.Second),
		OnCommit:        onCommit,
	}
}

func (c *Consensus) RecalculateF() {
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
	fmt.Printf("[BATS] Node %s: Membership Updated. Total Nodes:%d, F:%d\n", c.ID, len(c.PublicKeys), c.F)
}

// GetPeers returns a copy of the peers slice (thread-safe, acquires lock).
func (c *Consensus) GetPeers() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.getPeersLocked()
}

// getPeersLocked returns a copy of the peers slice. Caller MUST hold c.mu.
func (c *Consensus) getPeersLocked() []string {
	p := make([]string, len(c.Peers))
	copy(p, c.Peers)
	return p
}

func (c *Consensus) GetLeader() string {
	var allNodes []string
	totalWeight := 0
	for id, w := range c.Weights {
		allNodes = append(allNodes, id)
		totalWeight += w
	}
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
	return c.GetLeader() == c.ID
}

func (c *Consensus) Start(digest [64]byte) {
	c.mu.Lock()

	if !c.IsLeader() {
		c.mu.Unlock()
		return
	}

	msg := &types.ConsensusMessage{
		Type:   types.MessageType_PREPREPARE,
		View:   c.View,
		Digest: digest[:],
		NodeId: c.ID,
	}
	c.sign(msg)

	// CRITICAL FIX: Use getPeersLocked() since we already hold c.mu.
	// The old code called GetPeers() which tries to Lock() again = DEADLOCK.
	peers := c.getPeersLocked()
	c.mu.Unlock()

	// Parallel fan-out to all peers
	c.Network.Broadcast(peers, msg)

	// Leader also processes its own PrePrepare
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
		Digest: make([]byte, 64),
		NodeId: c.ID,
	}
	c.sign(msg)
	peers := c.getPeersLocked()
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

	// Verify sender identity and signature
	if pub, ok := c.PublicKeys[msg.NodeId]; ok {
		sig := msg.Signature
		msg.Signature = nil
		data, _ := proto.Marshal(msg)
		msg.Signature = sig

		if !crypto.Verify(pub, data, sig) {
			fmt.Printf("[BATS] Node %s: SIGNATURE FAILED from %s. Dropping.\n", c.ID, msg.NodeId)
			c.mu.Unlock()
			return
		}
	} else {
		fmt.Printf("[BATS] Node %s: UNKNOWN NODE %s. Dropping.\n", c.ID, msg.NodeId)
		c.mu.Unlock()
		return
	}

	if msg.View < c.View {
		c.mu.Unlock()
		return
	}

	if msg.Type == types.MessageType_PREPREPARE || msg.Type == types.MessageType_PREPARE || msg.Type == types.MessageType_COMMIT {
		if len(msg.Digest) != 64 {
			c.mu.Unlock()
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
		// CRITICAL FIX: use getPeersLocked() to avoid deadlock
		peers := c.getPeersLocked()
		c.mu.Unlock()
		c.Network.Broadcast(peers, reply)

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
			peers := c.getPeersLocked()
			c.mu.Unlock()
			c.Network.Broadcast(peers, commit)
			return
		}
		c.mu.Unlock()

	case types.MessageType_COMMIT:
		if _, ok := c.Commit[key]; !ok {
			c.Commit[key] = make(map[string]bool)
		}
		c.Commit[key][msg.NodeId] = true

		if len(c.Commit[key]) >= 2*c.F+1 {
			fmt.Printf("[BATS] CONSENSUS REACHED [View:%d]: %s\n", c.View, key[:16]+"...")
			c.WAL.Write("COMMITTED:" + key)
			c.resetTimer()

			if c.OnCommit != nil {
				var d [64]byte
				copy(d[:], msg.Digest)
				c.mu.Unlock()
				c.OnCommit(d)
				return
			}
		}
		c.mu.Unlock()

	case types.MessageType_VIEW_CHANGE:
		targetView := msg.View
		if _, ok := c.ViewChangeVotes[targetView]; !ok {
			c.ViewChangeVotes[targetView] = make(map[string]bool)
		}
		c.ViewChangeVotes[targetView][msg.NodeId] = true

		if len(c.ViewChangeVotes[targetView]) >= 2*c.F+1 && targetView > c.View {
			fmt.Printf("[BATS] VIEW CHANGE to View %d\n", targetView)
			c.View = targetView
			c.Prepare = make(map[string]map[string]bool)
			c.Commit = make(map[string]map[string]bool)
			c.resetTimer()
		}
		c.mu.Unlock()
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
		c.mu.Lock()
		if c.IsLeader() {
			c.mu.Unlock()
			c.Heartbeat()
		} else {
			fmt.Printf("[BATS] Node %s: Leader timeout. Initiating View Change.\n", c.ID)
			nextView := c.View + 1
			msg := &types.ConsensusMessage{
				Type:   types.MessageType_VIEW_CHANGE,
				View:   nextView,
				NodeId: c.ID,
			}
			c.sign(msg)
			peers := c.getPeersLocked()
			c.Network.Broadcast(peers, msg)
			c.mu.Unlock()
		}
		c.resetTimer()
	}
}
