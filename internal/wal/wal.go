package wal

import (
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

type WALEntry struct {
	Timestamp       int64           `json:"timestamp"`
	ActionHash      string          `json:"action_hash"`
	ProposingAgent  string          `json:"proposing_agent"`
	ConsensusResult string          `json:"consensus_result"`
	NodeVotes       map[string]bool `json:"node_votes,omitempty"`
	PrevHash        string          `json:"prev_hash"`
	EntryHash       string          `json:"entry_hash"`
}

type WAL struct {
	mu       sync.Mutex
	file     *os.File
	path     string
	count    int
	lastHash string
}

func NewWAL(path string) (*WAL, error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, err
	}

	w := &WAL{
		file:     f,
		path:     path,
		lastHash: "0000000000000000000000000000000000000000000000000000000000000000",
	}

	// Simplistic Last-Hash Recovery: In production, read backwards.
	// We read the entire file looking for the last EntryHash.
	content, _ := os.ReadFile(path)
	lines := strings.Split(string(content), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			var lastEntry WALEntry
			if err := json.Unmarshal([]byte(lines[i]), &lastEntry); err == nil && lastEntry.EntryHash != "" {
				w.lastHash = lastEntry.EntryHash
				break
			}
		}
	}

	return w, nil
}

// Write the hash-chained entry to the log
func (w *WAL) Append(actionHash, agentID, result string, votes map[string]bool) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	entry := WALEntry{
		Timestamp:       time.Now().UnixNano(),
		ActionHash:      actionHash,
		ProposingAgent:  agentID,
		ConsensusResult: result,
		NodeVotes:       votes,
		PrevHash:        w.lastHash,
	}

	// Cryptographic Hash Chain: SHA256(PrevHash + Timestamp + ActionHash + Result)
	raw := fmt.Sprintf("%s|%d|%s|%s", w.lastHash, entry.Timestamp, entry.ActionHash, entry.ConsensusResult)
	hash := sha256.Sum256([]byte(raw))
	entry.EntryHash = hex.EncodeToString(hash[:])

	w.lastHash = entry.EntryHash
	w.count++

	data, _ := json.Marshal(entry)
	_, err := w.file.Write(append(data, '\n'))

	if w.count > 1000 { // Auto-rotate threshold
		w.rotate()
	}
	return err
}

// Keep backward compatibility for standard string writes
func (w *WAL) Write(v interface{}) error {
	if s, ok := v.(string); ok {
		return w.Append(s, "system", "UNKNOWN", nil)
	}
	data, _ := json.Marshal(v)
	return w.Append(string(data), "system", "UNKNOWN", nil)
}

func (w *WAL) rotate() {
	w.file.Close()
	os.Rename(w.path, w.path+".old")
	f, _ := os.OpenFile(w.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	w.file = f
	w.count = 0
}

// ExportJSON writes the entire WAL history array to w
func (w *WAL) ExportJSON(writer io.Writer) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	content, err := os.ReadFile(w.path)
	if err != nil {
		return err
	}

	var entries []WALEntry
	lines := strings.Split(string(content), "\n")
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		var e WALEntry
		if err := json.Unmarshal([]byte(l), &e); err == nil {
			entries = append(entries, e)
		}
	}

	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	_, err = writer.Write(data)
	return err
}

// ExportCSV exports the WAL history as a CSV for compliance tracking
func (w *WAL) ExportCSV(writer io.Writer) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	content, err := os.ReadFile(w.path)
	if err != nil {
		return err
	}

	csvWriter := csv.NewWriter(writer)
	// Write Header
	csvWriter.Write([]string{"Timestamp", "ActionHash", "Agent", "Result", "PrevHash", "EntryHash"})

	lines := strings.Split(string(content), "\n")
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		var e WALEntry
		if err := json.Unmarshal([]byte(l), &e); err == nil {
			csvWriter.Write([]string{
				fmt.Sprintf("%d", e.Timestamp),
				e.ActionHash,
				e.ProposingAgent,
				e.ConsensusResult,
				e.PrevHash,
				e.EntryHash,
			})
		}
	}
	csvWriter.Flush()
	return csvWriter.Error()
}
