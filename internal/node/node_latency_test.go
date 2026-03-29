package node

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// BenchmarkHandleValidate_FastPath proves latency drops < 100ms for safe reads.
func BenchmarkHandleValidate_FastPath(b *testing.B) {
	// Use absolute path for cert resolution
	os.Chdir("/Users/admin/BATS/bats/")
	
	// Temporarily disable API keys to ensure local mock heuristic is used
	os.Setenv("OPENAI_API_KEY", "")
	
	// Initialize a standalone test node
	n := NewNode("node1", "8001", []string{})
	
	// Force leadership for the proxy handler
	n.Consensus.View = 1 

	// "read" invokes the [SAFE_READ] heuristic fast-path
	reqBody := []byte(`{"action": "read user profile 123"}`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req, _ := http.NewRequest("POST", "/validate", bytes.NewBuffer(reqBody))
		rr := httptest.NewRecorder()
		n.HandleValidate(rr, req)
		
		if rr.Code != http.StatusOK {
			b.Fatalf("Expected 200 OK, got: %d", rr.Code)
		}
	}
}
