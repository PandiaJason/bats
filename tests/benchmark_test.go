package tests

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"testing"
	"time"

	"bats/internal/node"
)

func percentiles(latencies []time.Duration) (p50, p95, p99 time.Duration) {
	if len(latencies) == 0 {
		return 0, 0, 0
	}
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	idx := func(pct float64) int {
		i := int(float64(len(latencies)) * pct)
		if i >= len(latencies) {
			i = len(latencies) - 1
		}
		return i
	}
	return latencies[idx(0.50)], latencies[idx(0.95)], latencies[idx(0.99)]
}

func fireRequests(client *http.Client, url string, payload []byte, n int) []time.Duration {
	var latencies []time.Duration
	for i := 0; i < n; i++ {
		start := time.Now()

		req, _ := http.NewRequest("POST", url, bytes.NewBuffer(payload))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-BATS-Nonce", fmt.Sprintf("bench-%d-%d", time.Now().UnixNano(), i))
		req.Header.Set("X-BATS-Timestamp", fmt.Sprintf("%d", time.Now().Unix()))

		resp, err := client.Do(req)
		elapsed := time.Since(start)
		if err != nil {
			fmt.Printf("  [ERR] request %d failed: %v\n", i, err)
			continue
		}

		var body map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&body)
		resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			latencies = append(latencies, elapsed)
		}
		time.Sleep(10 * time.Millisecond) // breathing room between requests
	}
	return latencies
}

// TestBenchmarkLatency boots a real 4-node BATS cluster and measures
// end-to-end latency for fast-path reads, synchronous PBFT writes,
// and blocked unsafe actions. Targets: reads <100ms, writes <500ms.
func TestBenchmarkLatency(t *testing.T) {
	os.Chdir("/Users/admin/BATS/bats/")
	os.Setenv("OPENAI_API_KEY", "")

	peers := []string{
		"localhost:8001",
		"localhost:8002",
		"localhost:8003",
		"localhost:8004",
	}

	n1 := node.NewNode("node1", "8001", peers)
	n2 := node.NewNode("node2", "8002", peers)
	n3 := node.NewNode("node3", "8003", peers)
	n4 := node.NewNode("node4", "8004", peers)

	go n1.Start("8001")
	go n2.Start("8002")
	go n3.Start("8003")
	go n4.Start("8004")

	// Wait for TLS listeners to be ready
	time.Sleep(2 * time.Second)

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig:     &tls.Config{InsecureSkipVerify: true},
			MaxIdleConnsPerHost: 20,
			ForceAttemptHTTP2:   true,
		},
		Timeout: 2 * time.Second,
	}

	iterations := 10
	url := "https://localhost:8001/validate"

	fmt.Println("\n=== BATS CLUSTER LATENCY BENCHMARK (4 nodes, HTTP/2, PBFT) ===")
	fmt.Println()

	// --- SAFE_READ: optimistic fast-path ---
	fmt.Println("[1/3] Benchmarking SAFE_READ (fast-path)...")
	fastLat := fireRequests(client, url, []byte(`{"action":"read user profile 123"}`), iterations)
	fP50, fP95, fP99 := percentiles(fastLat)
	fmt.Printf("      %d/%d succeeded | p50=%v p95=%v p99=%v\n", len(fastLat), iterations, fP50, fP95, fP99)

	// --- SAFE write: synchronous PBFT ---
	fmt.Println("[2/3] Benchmarking SAFE write (sync PBFT)...")
	syncLat := fireRequests(client, url, []byte(`{"action":"update user profile 123"}`), iterations)
	sP50, sP95, sP99 := percentiles(syncLat)
	fmt.Printf("      %d/%d succeeded | p50=%v p95=%v p99=%v\n", len(syncLat), iterations, sP50, sP95, sP99)

	// --- UNSAFE: immediate block ---
	fmt.Println("[3/3] Benchmarking UNSAFE (immediate reject)...")
	unsafeLat := fireRequests(client, url, []byte(`{"action":"DROP TABLE users"}`), iterations)
	uP50, uP95, uP99 := percentiles(unsafeLat)
	fmt.Printf("      %d/%d succeeded | p50=%v p95=%v p99=%v\n", len(unsafeLat), iterations, uP50, uP95, uP99)

	// --- Results Table ---
	fmt.Printf("\n📊 BENCHMARK RESULTS (4-node cluster)\n")
	fmt.Printf("%-30s | %-12s | %-12s | %-12s\n", "Action Type", "p50", "p95", "p99")
	fmt.Printf("-------------------------------|--------------|--------------|-------------\n")
	fmt.Printf("%-30s | %-12v | %-12v | %-12v\n", "SAFE_READ (Fast Bypass)", fP50, fP95, fP99)
	fmt.Printf("%-30s | %-12v | %-12v | %-12v\n", "SAFE (Sync PBFT Write)", sP50, sP95, sP99)
	fmt.Printf("%-30s | %-12v | %-12v | %-12v\n", "UNSAFE (Immediate Reject)", uP50, uP95, uP99)
	fmt.Println()

	// --- Assertions ---
	if fP50 > 100*time.Millisecond {
		t.Errorf("FAIL: SAFE_READ p50=%v exceeds 100ms target", fP50)
	}
	if len(syncLat) > 0 && sP50 > 500*time.Millisecond {
		t.Errorf("FAIL: SAFE write p50=%v exceeds 500ms target", sP50)
	}
	if len(syncLat) == 0 {
		t.Errorf("FAIL: No SAFE write requests completed (PBFT still broken)")
	}
	if len(syncLat) > 0 && sP99 > 800*time.Millisecond {
		t.Errorf("FAIL: SAFE write p99=%v exceeds 800ms target", sP99)
	}

	// Write results artifact
	table := fmt.Sprintf(
		"| Action Type | p50 | p95 | p99 |\n"+
			"|---|---|---|---|\n"+
			"| SAFE_READ (Fast Bypass) | %v | %v | %v |\n"+
			"| SAFE (Sync PBFT Write) | %v | %v | %v |\n"+
			"| UNSAFE (Immediate Reject) | %v | %v | %v |\n",
		fP50, fP95, fP99, sP50, sP95, sP99, uP50, uP95, uP99,
	)
	os.WriteFile("benchmark_results.md", []byte(table), 0644)
}
