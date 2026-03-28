package main

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"bats/internal/types"

	"google.golang.org/protobuf/proto"
)

type NodeStatus struct {
	ID       string `json:"id"`
	Port     string `json:"port"`
	Alive    bool   `json:"alive"`
	View     uint64 `json:"view"`
	IsLeader bool   `json:"is_leader"`
}

var (
	nodes     = make(map[string]*NodeStatus)
	nodesMu   sync.RWMutex
	peerList  []string
	client    *http.Client
)

func init() {
	caCert, _ := os.ReadFile("certs/ca.crt")
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCert)

	client = &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs: caCertPool,
			},
		},
		Timeout: 2 * time.Second,
	}
}

func main() {
	peertsEnv := os.Getenv("PEERS")
	if peertsEnv == "" {
		peerList = []string{"localhost:8001", "localhost:8002", "localhost:8003", "localhost:8004"}
	} else {
		peerList = strings.Split(peertsEnv, ",")
	}

	go pollNodes()

	http.Handle("/", http.FileServer(http.Dir("./internal/dashboard/static")))

	http.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		nodesMu.RLock()
		defer nodesMu.RUnlock()
		json.NewEncoder(w).Encode(nodes)
	})

	fmt.Println("Dashboard running on :8080")
	http.ListenAndServe(":8080", nil)
}

func pollNodes() {
	for {
		for _, addr := range peerList {
			go func(a string) {
				resp, err := client.Get("https://" + a + "/status")
				if err != nil {
					nodesMu.Lock()
					if _, ok := nodes[a]; !ok {
						nodes[a] = &NodeStatus{ID: "Unknown", Port: a, Alive: false}
					} else {
						nodes[a].Alive = false
					}
					nodesMu.Unlock()
					return
				}
				defer resp.Body.Close()

				body, _ := io.ReadAll(resp.Body)
				var status types.NodeStatus
				if err := proto.Unmarshal(body, &status); err != nil {
					return
				}

				nodesMu.Lock()
				nodes[a] = &NodeStatus{
					ID:       status.Id,
					Port:     a,
					Alive:    true,
					View:     status.View,
					IsLeader: status.IsLeader,
				}
				nodesMu.Unlock()
			}(addr)
		}
		time.Sleep(2 * time.Second)
	}
}
