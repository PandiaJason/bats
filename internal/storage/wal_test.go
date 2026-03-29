package storage

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestHashChainedWAL(t *testing.T) {
	tmpFile := "test_wal_log.json"
	defer os.Remove(tmpFile)
	defer os.Remove(tmpFile + ".old") // Cleanup rotation

	wal, err := NewWAL(tmpFile)
	if err != nil {
		t.Fatalf("Failed to create WAL: %v", err)
	}

	// 1. Append first entry
	err = wal.Append("actionHash123", "agent-007", "COMMITTED", map[string]bool{"node1": true})
	if err != nil {
		t.Fatalf("Failed to append: %v", err)
	}

	// 2. Append second entry to check chaining
	err = wal.Append("actionHash456", "agent-008", "BLOCKED", nil)
	if err != nil {
		t.Fatalf("Failed to append second: %v", err)
	}

	// 3. Read back to verify hashes
	content, _ := os.ReadFile(tmpFile)
	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	if len(lines) != 2 {
		t.Fatalf("Expected 2 lines, got %d", len(lines))
	}

	var entry1, entry2 WALEntry
	json.Unmarshal([]byte(lines[0]), &entry1)
	json.Unmarshal([]byte(lines[1]), &entry2)

	if entry1.EntryHash == "" {
		t.Fatal("First entry hash empty")
	}
	if entry2.PrevHash != entry1.EntryHash {
		t.Fatalf("Hash chain broken! Entry2 Prev[%s] != Entry1 Hash[%s]", entry2.PrevHash, entry1.EntryHash)
	}

	// 4. Test Exports
	var jsonBuf bytes.Buffer
	err = wal.ExportJSON(&jsonBuf)
	if err != nil {
		t.Fatalf("ExportJSON failed: %v", err)
	}
	if !strings.Contains(jsonBuf.String(), "actionHash456") {
		t.Fatalf("Exported JSON missing content")
	}

	var csvBuf bytes.Buffer
	err = wal.ExportCSV(&csvBuf)
	if err != nil {
		t.Fatalf("ExportCSV failed: %v", err)
	}
	if !strings.Contains(csvBuf.String(), "actionHash123") {
		t.Fatalf("Exported CSV missing content")
	}
}
