package tests

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"bats/internal/wal"
)

func TestHashChainedWAL_Security(t *testing.T) {
	tmpFile := "test_security_wal.json"
	defer os.Remove(tmpFile)

	log, err := wal.NewWAL(tmpFile)
	if err != nil {
		t.Fatalf("Failed starting WAL: %v", err)
	}

	// 1. Log two separate transactions
	err = log.Append("action-sync-cluster", "agent-x", "APPROVED", map[string]bool{"node1": true})
	if err != nil {
		t.Fatalf("Failed append 1: %v", err)
	}

	err = log.Append("action-delete-sys", "agent-y", "BLOCKED", nil)
	if err != nil {
		t.Fatalf("Failed append 2: %v", err)
	}

	// 2. Read back from disk to verify cryptographic chaining
	content, _ := os.ReadFile(tmpFile)
	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	if len(lines) != 2 {
		t.Fatalf("Expected 2 transactions written, found %d", len(lines))
	}

	var tx1, tx2 wal.WALEntry
	json.Unmarshal([]byte(lines[0]), &tx1)
	json.Unmarshal([]byte(lines[1]), &tx2)

	if tx1.EntryHash == "" || tx2.EntryHash == "" {
		t.Fatalf("Missing calculated EntryHashes")
	}
	
	if tx2.PrevHash != tx1.EntryHash {
		t.Fatalf(
			"CRYPTOGRAPHIC TAMPER DETECTED: Chain Broken!\nTx2.PrevHash = %s\nTx1.EntryHash = %s",
			tx2.PrevHash, tx1.EntryHash,
		)
	}

	// 3. Test Exports (JSON and CSV)
	var jsonBuf bytes.Buffer
	if err := log.ExportJSON(&jsonBuf); err != nil {
		t.Fatalf("JSON Export failed: %v", err)
	}
	if !strings.Contains(jsonBuf.String(), "action-sync-cluster") {
		t.Fatalf("JSON export missing payload")
	}

	var csvBuf bytes.Buffer
	if err := log.ExportCSV(&csvBuf); err != nil {
		t.Fatalf("CSV Export failed: %v", err)
	}
	if !strings.Contains(csvBuf.String(), "agent-x") {
		t.Fatalf("CSV export missing node columns")
	}
}
