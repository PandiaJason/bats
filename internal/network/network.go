package network

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"bats/internal/types"

	"google.golang.org/protobuf/proto"
)

// Default network timeout per hop. Override with WAND_HOP_TIMEOUT_MS env.
var HopTimeout = 200 * time.Millisecond

func init() {
	if v := os.Getenv("WAND_HOP_TIMEOUT_MS"); v != "" {
		if ms, err := strconv.Atoi(v); err == nil {
			HopTimeout = time.Duration(ms) * time.Millisecond
		}
	}
}

type Client struct {
	// Primary: HTTP/2 over TLS — works reliably with self-signed certs on localhost.
	// This is what http.ListenAndServeTLS serves on every node.
	h2Client *http.Client
}

func NewClient(nodeID string) *Client {
	caCert, _ := os.ReadFile("certs/ca.crt")
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCert)

	cert, _ := tls.LoadX509KeyPair("certs/"+nodeID+".crt", "certs/"+nodeID+".key")

	// InsecureSkipVerify is opt-in for development only.
	// In production, proper CA verification is enforced.
	insecure := os.Getenv("WAND_TLS_INSECURE") == "1"

	tlsConfig := &tls.Config{
		RootCAs:            caCertPool,
		Certificates:       []tls.Certificate{cert},
		InsecureSkipVerify: insecure,
	}

	return &Client{
		h2Client: &http.Client{
			Transport: &http.Transport{
				TLSClientConfig:     tlsConfig,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     30 * time.Second,
				// Force HTTP/2 by configuring TLS with h2 ALPN
				ForceAttemptHTTP2: true,
			},
			Timeout: HopTimeout,
		},
	}
}

// GetHTTPClient exposes the transport for forwarding and external calls.
func (c *Client) GetHTTPClient() *http.Client {
	return c.h2Client
}

// Send delivers a single message to a peer. Returns error on failure.
func (c *Client) Send(addr string, msg *types.ConsensusMessage) error {
	data, err := proto.Marshal(msg)
	if err != nil {
		return err
	}

	resp, err := c.h2Client.Post("https://"+addr+"/validate", "application/x-protobuf", bytes.NewBuffer(data))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

// Broadcast sends a message to all peers in parallel using goroutine
// fan-out. Each peer gets a single attempt with the configured hop timeout.
// A WaitGroup ensures all sends are dispatched before returning.
func (c *Client) Broadcast(peers []string, msg *types.ConsensusMessage) {
	var wg sync.WaitGroup
	for _, p := range peers {
		wg.Add(1)
		go func(peer string) {
			defer wg.Done()
			if err := c.Send(peer, msg); err != nil {
				fmt.Printf("[NET] Failed to reach %s: %v\n", peer, err)
			}
		}(p)
	}
	// Don't block the caller forever — but do wait a bounded time
	// for the parallel sends to complete or timeout individually.
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(HopTimeout + 50*time.Millisecond):
	}
}
