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

func fireRequests(client *http.Client, url string, payload []byte, n int) (latencies []time.Duration, approved int, blocked int) {
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

		var result map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&result)
		resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			latencies = append(latencies, elapsed)
			if result["approved"] == true {
				approved++
			} else {
				blocked++
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	return
}

// warmup sends throwaway requests to pre-establish TLS connections and
// warm the exact /validate endpoint path.
func warmup(client *http.Client, url string) {
	payloads := []string{
		`{"action":"warmup read a file"}`,
		`{"action":"warmup save a profile"}`,
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

// TestBenchmarkLatency boots a real 4-node WAND cluster and measures
// end-to-end latency with proper warmup to eliminate cold-start TLS skew.
func TestBenchmarkLatency(t *testing.T) {
	os.Chdir("/Users/admin/BATS/bats/")
	os.Setenv("OPENAI_API_KEY", "")
	os.Setenv("WAND_TLS_INSECURE", "1")

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
	fmt.Println("\n[WARMUP] Pre-establishing TLS connections...")
	warmup(client, url)
	fmt.Println("[WARMUP] Done. Starting measured benchmark.")

	iterations := 20

	fmt.Println("=== WAND CLUSTER LATENCY BENCHMARK (4 nodes, HTTP/2, Deterministic Policy) ===")

	// --- SAFE_READ: fast-path (should be APPROVED) ---
	fmt.Println("[1/3] SAFE_READ (fast-path)...")
	fastLat, fastApproved, fastBlocked := fireRequests(client, url, []byte(`{"action":"read user profile 123"}`), iterations)
	fP50, fP95, fP99 := percentiles(fastLat)
	fmt.Printf("      %d/%d ok (approved=%d blocked=%d) | p50=%v p95=%v p99=%v\n", len(fastLat), iterations, fastApproved, fastBlocked, fP50, fP95, fP99)

	// --- SAFE_WRITE: should also be APPROVED (natural language, NOT SQL) ---
	fmt.Println("[2/3] SAFE_WRITE (deterministic policy)...")
	syncLat, syncApproved, syncBlocked := fireRequests(client, url, []byte(`{"action":"save user profile 123"}`), iterations)
	sP50, sP95, sP99 := percentiles(syncLat)
	fmt.Printf("      %d/%d ok (approved=%d blocked=%d) | p50=%v p95=%v p99=%v\n", len(syncLat), iterations, syncApproved, syncBlocked, sP50, sP95, sP99)

	// --- UNSAFE: immediate block ---
	fmt.Println("[3/3] UNSAFE (immediate reject)...")
	unsafeLat, _, unsafeBlocked := fireRequests(client, url, []byte(`{"action":"DROP TABLE users"}`), iterations)
	uP50, uP95, uP99 := percentiles(unsafeLat)
	fmt.Printf("      %d/%d ok (blocked=%d) | p50=%v p95=%v p99=%v\n", len(unsafeLat), iterations, unsafeBlocked, uP50, uP95, uP99)

	// --- Results ---
	fmt.Printf("\n📊 BENCHMARK RESULTS (4-node cluster, post-warmup)\n")
	fmt.Printf("%-30s | %-12s | %-12s | %-12s\n", "Action Type", "p50", "p95", "p99")
	fmt.Printf("-------------------------------|--------------|--------------|-------------\n")
	fmt.Printf("%-30s | %-12v | %-12v | %-12v\n", "SAFE_READ (Policy Approved)", fP50, fP95, fP99)
	fmt.Printf("%-30s | %-12v | %-12v | %-12v\n", "SAFE_WRITE (Policy Approved)", sP50, sP95, sP99)
	fmt.Printf("%-30s | %-12v | %-12v | %-12v\n", "UNSAFE (Immediate Reject)", uP50, uP95, uP99)
	fmt.Println()

	// --- Correctness Assertions ---
	if fastApproved < iterations/2 {
		t.Errorf("FAIL: SAFE_READ should be mostly approved, only %d/%d approved", fastApproved, len(fastLat))
	}
	if syncApproved < iterations/2 {
		t.Errorf("FAIL: SAFE_WRITE should be mostly approved, only %d/%d approved", syncApproved, len(syncLat))
	}
	if unsafeBlocked < iterations/2 {
		t.Errorf("FAIL: UNSAFE should be mostly blocked, only %d/%d blocked", unsafeBlocked, len(unsafeLat))
	}

	// --- Latency Assertions (relaxed for CI — TLS over localhost is ~2-5ms post-warmup) ---
	if fP50 > 500*time.Millisecond {
		t.Errorf("FAIL: SAFE_READ p50=%v exceeds 500ms target", fP50)
	}
	if sP50 > 500*time.Millisecond {
		t.Errorf("FAIL: SAFE_WRITE p50=%v exceeds 500ms target", sP50)
	}

	// Write results artifact
	table := fmt.Sprintf(
		"| Action Type | p50 | p95 | p99 |\n"+
			"|---|---|---|---|\n"+
			"| SAFE_READ (Policy Approved) | %v | %v | %v |\n"+
			"| SAFE_WRITE (Policy Approved) | %v | %v | %v |\n"+
			"| UNSAFE (Immediate Reject) | %v | %v | %v |\n",
		fP50, fP95, fP99, sP50, sP95, sP99, uP50, uP95, uP99,
	)
	os.WriteFile("benchmark_results.md", []byte(table), 0644)

	results := map[string]interface{}{
		"safe_read":  map[string]string{"p50": fP50.String(), "p95": fP95.String(), "p99": fP99.String()},
		"safe_write": map[string]string{"p50": sP50.String(), "p95": sP95.String(), "p99": sP99.String()},
		"unsafe":     map[string]string{"p50": uP50.String(), "p95": uP95.String(), "p99": uP99.String()},
	}
	jsonData, _ := json.MarshalIndent(results, "", "  ")
	os.WriteFile("benchmark_results.json", jsonData, 0644)
}
