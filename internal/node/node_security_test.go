package node

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func TestSecurityHeaders_Missing(t *testing.T) {
	os.Chdir("../../")
	n := NewNode("node1", "8001", []string{})

	req, _ := http.NewRequest("POST", "/validate", bytes.NewBuffer([]byte(`{"action":"test"}`)))
	rr := httptest.NewRecorder()

	handler := n.requireSecurityHeaders(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("Expected 401 Unauthorized for missing headers, got %v", rr.Code)
	}
}

func TestSecurityHeaders_ExpiredTimestamp(t *testing.T) {
	n := NewNode("node1", "8001", []string{})

	req, _ := http.NewRequest("POST", "/validate", bytes.NewBuffer([]byte(`{"action":"test"}`)))
	
	// Create a timestamp from 5 minutes ago (300 seconds)
	oldTime := time.Now().Unix() - 300
	req.Header.Set("X-BATS-Timestamp", fmt.Sprintf("%d", oldTime))
	req.Header.Set("X-BATS-Nonce", "random-nonce-123")
	
	rr := httptest.NewRecorder()
	handler := n.requireSecurityHeaders(func(w http.ResponseWriter, r *http.Request) {})
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("Expected 401 for expired timestamp drift")
	}
}

func TestSecurityHeaders_ReplayNonce(t *testing.T) {
	n := NewNode("node1", "8001", []string{})

	req, _ := http.NewRequest("POST", "/validate", bytes.NewBuffer([]byte(`{"action":"test"}`)))
	currentTime := time.Now().Unix()
	
	req.Header.Set("X-BATS-Timestamp", fmt.Sprintf("%d", currentTime))
	req.Header.Set("X-BATS-Nonce", "replay-nonce-444")
	
	// First request succeeds
	rr1 := httptest.NewRecorder()
	handler := n.requireSecurityHeaders(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK) // simulate success
	})
	handler.ServeHTTP(rr1, req)

	if rr1.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for first valid request, got %v", rr1.Code)
	}

	// Second request with same nonce fails
	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, req)

	if rr2.Code != http.StatusUnauthorized {
		t.Fatalf("Expected 401 Unauthorized for replayed Nonce! Got %v", rr2.Code)
	}
}
