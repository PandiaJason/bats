package node

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// TestFastPath_SafeRead verifies that a safe read action gets approved
// via the optimistic fast-path without blocking on PBFT consensus.
func TestFastPath_SafeRead(t *testing.T) {
	os.Chdir("/Users/admin/BATS/bats/")
	os.Setenv("OPENAI_API_KEY", "")

	n := NewNode("node1", "8001", []string{})

	body := []byte(`{"action": "read user profile 123"}`)
	req, _ := http.NewRequest("POST", "/validate", bytes.NewBuffer(body))
	rr := httptest.NewRecorder()
	n.HandleValidate(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK, got %d. Body: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(rr.Body.Bytes(), &resp)

	if resp["approved"] != true {
		t.Fatalf("Expected approved=true, got %v", resp["approved"])
	}
	if resp["fast_path"] != true {
		t.Fatalf("Expected fast_path=true, got %v", resp["fast_path"])
	}
	conf, ok := resp["confidence"].(float64)
	if !ok || conf < 0.95 {
		t.Fatalf("Expected confidence >= 0.95, got %v", resp["confidence"])
	}
}

// TestSyncPath_SafeWrite verifies that a safe state-mutating action
// goes through the synchronous PBFT path (and times out in standalone mode).
func TestSyncPath_SafeWrite(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping sync-path test in short mode (10s timeout)")
	}
	os.Chdir("/Users/admin/BATS/bats/")
	os.Setenv("OPENAI_API_KEY", "")

	n := NewNode("node1", "8001", []string{})

	// "update" is SAFE but not SAFE_READ, so it goes through sync PBFT
	body := []byte(`{"action": "update user profile 123"}`)
	req, _ := http.NewRequest("POST", "/validate", bytes.NewBuffer(body))
	rr := httptest.NewRecorder()
	n.HandleValidate(rr, req)

	// In standalone mode, this will timeout because there is no quorum
	var resp map[string]interface{}
	json.Unmarshal(rr.Body.Bytes(), &resp)

	if resp["approved"] == true {
		// If somehow approved, verify it was NOT fast-path
		if resp["fast_path"] == true {
			t.Fatal("Write action must never use fast-path")
		}
	}
}

// TestBlockedAction verifies that unsafe actions are immediately rejected.
func TestBlockedAction(t *testing.T) {
	os.Chdir("/Users/admin/BATS/bats/")
	os.Setenv("OPENAI_API_KEY", "")

	n := NewNode("node1", "8001", []string{})

	body := []byte(`{"action": "rm -rf /etc/shadow"}`)
	req, _ := http.NewRequest("POST", "/validate", bytes.NewBuffer(body))
	rr := httptest.NewRecorder()
	n.HandleValidate(rr, req)

	var resp map[string]interface{}
	json.Unmarshal(rr.Body.Bytes(), &resp)

	if resp["approved"] != false {
		t.Fatalf("Expected UNSAFE action to be blocked, got approved=%v", resp["approved"])
	}
}

// BenchmarkHandleValidate_FastPath measures the end-to-end latency of the
// optimistic fast-path for safe read actions. Target: under 100ms (100,000,000 ns).
func BenchmarkHandleValidate_FastPath(b *testing.B) {
	os.Chdir("/Users/admin/BATS/bats/")
	os.Setenv("OPENAI_API_KEY", "")

	n := NewNode("node1", "8001", []string{})
	reqBody := []byte(`{"action": "read user profile 123"}`)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		req, _ := http.NewRequest("POST", "/validate", bytes.NewBuffer(reqBody))
		rr := httptest.NewRecorder()
		n.HandleValidate(rr, req)

		if rr.Code != http.StatusOK {
			b.Fatalf("Expected 200 OK, got: %d", rr.Code)
		}
	}
}

// BenchmarkHandleValidate_BlockedPath measures the rejection latency for unsafe actions.
func BenchmarkHandleValidate_BlockedPath(b *testing.B) {
	os.Chdir("/Users/admin/BATS/bats/")
	os.Setenv("OPENAI_API_KEY", "")

	n := NewNode("node1", "8001", []string{})
	reqBody := []byte(`{"action": "DROP TABLE users"}`)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		req, _ := http.NewRequest("POST", "/validate", bytes.NewBuffer(reqBody))
		rr := httptest.NewRecorder()
		n.HandleValidate(rr, req)

		if rr.Code != http.StatusOK {
			b.Fatalf("Expected 200 OK, got: %d", rr.Code)
		}
	}
}
