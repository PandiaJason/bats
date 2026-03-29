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
			continue
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			latencies = append(latencies, elapsed)
		}
		time.Sleep(5 * time.Millisecond)
	}
	return latencies
}

// warmup sends throwaway requests to pre-establish TLS connections and
// warm the exact /validate endpoint path. Without this, the first request
// pays ~80ms TLS handshake + HTTP/2 SETTINGS exchange.
func warmup(client *http.Client, url string) {
	// Warm all three action paths to populate the connection pool
	payloads := []string{
		`{"action":"warmup read"}`,
		`{"action":"warmup update"}`,
		`{"action":"DROP warmup"}`,
	}
	for _, p := range payloads {
		for i := 0; i < 3; i++ {
			req, _ := http.NewRequest("POST", url, bytes.NewBufferString(p))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-BATS-Nonce", fmt.Sprintf("warmup-%d-%d", time.Now().UnixNano(), i))
			req.Header.Set("X-BATS-Timestamp", fmt.Sprintf("%d", time.Now().Unix()))
			resp, err := client.Do(req)
			if err == nil {
				resp.Body.Close()
			}
		}
	}
}

// TestBenchmarkLatency boots a real 4-node BATS cluster and measures
// end-to-end latency with proper warmup to eliminate cold-start TLS skew.
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

	time.Sleep(2 * time.Second)

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig:     &tls.Config{InsecureSkipVerify: true},
			MaxIdleConnsPerHost: 20,
			ForceAttemptHTTP2:   true,
		},
		Timeout: 2 * time.Second,
	}

	url := "https://localhost:8001/validate"

	// Warmup: pre-establish TLS sessions and connection pools.
	// This eliminates the ~80ms cold-start spike on the first request.
	fmt.Println("\n[WARMUP] Pre-establishing TLS connections...")
	warmup(client, url)
	fmt.Println("[WARMUP] Done. Starting measured benchmark.")

	iterations := 20

	fmt.Println("=== BATS CLUSTER LATENCY BENCHMARK (4 nodes, HTTP/2, PBFT) ===")

	// --- SAFE_READ: fast-path ---
	fmt.Println("[1/3] SAFE_READ (fast-path)...")
	fastLat := fireRequests(client, url, []byte(`{"action":"read user profile 123"}`), iterations)
	fP50, fP95, fP99 := percentiles(fastLat)
	fmt.Printf("      %d/%d ok | p50=%v p95=%v p99=%v\n", len(fastLat), iterations, fP50, fP95, fP99)

	// --- SAFE write: sync PBFT ---
	fmt.Println("[2/3] SAFE write (sync PBFT)...")
	syncLat := fireRequests(client, url, []byte(`{"action":"update user profile 123"}`), iterations)
	sP50, sP95, sP99 := percentiles(syncLat)
	fmt.Printf("      %d/%d ok | p50=%v p95=%v p99=%v\n", len(syncLat), iterations, sP50, sP95, sP99)

	// --- UNSAFE: immediate block ---
	fmt.Println("[3/3] UNSAFE (immediate reject)...")
	unsafeLat := fireRequests(client, url, []byte(`{"action":"DROP TABLE users"}`), iterations)
	uP50, uP95, uP99 := percentiles(unsafeLat)
	fmt.Printf("      %d/%d ok | p50=%v p95=%v p99=%v\n", len(unsafeLat), iterations, uP50, uP95, uP99)

	// --- Results ---
	fmt.Printf("\n📊 BENCHMARK RESULTS (4-node cluster, post-warmup)\n")
	fmt.Printf("%-30s | %-12s | %-12s | %-12s\n", "Action Type", "p50", "p95", "p99")
	fmt.Printf("-------------------------------|--------------|--------------|-------------\n")
	fmt.Printf("%-30s | %-12v | %-12v | %-12v\n", "SAFE_READ (Fast Bypass)", fP50, fP95, fP99)
	fmt.Printf("%-30s | %-12v | %-12v | %-12v\n", "SAFE Write (Sync PBFT)", sP50, sP95, sP99)
	fmt.Printf("%-30s | %-12v | %-12v | %-12v\n", "UNSAFE (Immediate Reject)", uP50, uP95, uP99)
	fmt.Println()

	// --- Assertions ---
	if fP50 > 10*time.Millisecond {
		t.Errorf("FAIL: SAFE_READ p50=%v exceeds 10ms target", fP50)
	}
	if fP95 > 20*time.Millisecond {
		t.Errorf("FAIL: SAFE_READ p95=%v exceeds 20ms target (TLS warmup issue?)", fP95)
	}
	if len(syncLat) == 0 {
		t.Errorf("FAIL: No SAFE writes completed (PBFT broken)")
	}
	if len(syncLat) > 0 && sP50 > 200*time.Millisecond {
		t.Errorf("FAIL: SAFE write p50=%v exceeds 200ms target", sP50)
	}
	if len(syncLat) > 0 && sP99 > 500*time.Millisecond {
		t.Errorf("FAIL: SAFE write p99=%v exceeds 500ms target", sP99)
	}

	// Write results artifact
	table := fmt.Sprintf(
		"| Action Type | p50 | p95 | p99 |\n"+
			"|---|---|---|---|\n"+
			"| SAFE_READ (Fast Bypass) | %v | %v | %v |\n"+
			"| SAFE Write (Sync PBFT) | %v | %v | %v |\n"+
			"| UNSAFE (Immediate Reject) | %v | %v | %v |\n",
		fP50, fP95, fP99, sP50, sP95, sP99, uP50, uP95, uP99,
	)
	os.WriteFile("benchmark_results.md", []byte(table), 0644)

	// Pretty summary for JSON consumers
	results := map[string]interface{}{
		"safe_read":  map[string]string{"p50": fP50.String(), "p95": fP95.String(), "p99": fP99.String()},
		"safe_write": map[string]string{"p50": sP50.String(), "p95": sP95.String(), "p99": sP99.String()},
		"unsafe":     map[string]string{"p50": uP50.String(), "p95": uP95.String(), "p99": uP99.String()},
	}
	jsonData, _ := json.MarshalIndent(results, "", "  ")
	os.WriteFile("benchmark_results.json", jsonData, 0644)
}
