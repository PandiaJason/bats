package node

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// TestFastPath_SafeRead verifies that a safe read action is immediately
// approved by the deterministic WAND policy engine.
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
}

// TestSyncPath_SafeWrite verifies that a safe state-mutating action
// that doesn't match any dangerous or risky pattern gets ALLOW verdict.
func TestSyncPath_SafeWrite(t *testing.T) {
	os.Chdir("/Users/admin/BATS/bats/")
	os.Setenv("OPENAI_API_KEY", "")

	n := NewNode("node1", "8001", []string{})

	// "save" doesn't match any block or challenge pattern
	body := []byte(`{"action": "save user profile 123"}`)
	req, _ := http.NewRequest("POST", "/validate", bytes.NewBuffer(body))
	rr := httptest.NewRecorder()
	n.HandleValidate(rr, req)

	var resp map[string]interface{}
	json.Unmarshal(rr.Body.Bytes(), &resp)

	if resp["approved"] != true {
		t.Fatalf("Expected approved=true for safe write action, got %v (decision=%v)", resp["approved"], resp["decision"])
	}
	if resp["decision"] != "ALLOW" {
		t.Fatalf("Expected decision=ALLOW, got %v", resp["decision"])
	}
}

// TestChallengeAction verifies that risky-but-legitimate actions get
// the CHALLENGE verdict, which requires user re-approval.
func TestChallengeAction(t *testing.T) {
	os.Chdir("/Users/admin/BATS/bats/")
	os.Setenv("OPENAI_API_KEY", "")

	n := NewNode("node1", "8001", []string{})

	body := []byte(`{"action": "git push --force origin main"}`)
	req, _ := http.NewRequest("POST", "/validate", bytes.NewBuffer(body))
	rr := httptest.NewRecorder()
	n.HandleValidate(rr, req)

	var resp map[string]interface{}
	json.Unmarshal(rr.Body.Bytes(), &resp)

	if resp["decision"] != "CHALLENGE" {
		t.Fatalf("Expected decision=CHALLENGE for force push, got %v", resp["decision"])
	}
	if resp["approved"] != false {
		t.Fatalf("CHALLENGE actions must have approved=false, got %v", resp["approved"])
	}
	if resp["challenge"] == nil || resp["challenge"] == "" {
		t.Fatal("CHALLENGE response must include a challenge message for the user")
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
// deterministic WAND policy evaluation for safe read actions.
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
