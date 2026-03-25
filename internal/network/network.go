package network

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"os"
	"time"

	"bats/internal/types"

	"github.com/quic-go/quic-go/http3"
	"google.golang.org/protobuf/proto"
)

var h3Client *http.Client

func init() {
	caCert, _ := os.ReadFile("certs/ca.crt")
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCert)

	tlsConfig := &tls.Config{
		RootCAs:            caCertPool,
		InsecureSkipVerify: true,
		NextProtos:         []string{"h3"}, // HTTP/3
	}

	// 🛡️ HTTP/3 Transport for production-grade low-latency communication.
	// In quic-go v0.59.0, http3.Transport is the primary entry point.
	h3Client = &http.Client{
		Transport: &http3.Transport{
			TLSClientConfig: tlsConfig,
		},
		Timeout: 5 * time.Second,
	}
}

func Send(addr string, msg *types.ConsensusMessage) error {
	data, err := proto.Marshal(msg)
	if err != nil {
		return err
	}

	// ⚡ Using HTTP/3 (QUIC-based) for production-grade transport
	resp, err := h3Client.Post("https://"+addr+"/consensus", "application/x-protobuf", bytes.NewBuffer(data))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

func Broadcast(peers []string, msg *types.ConsensusMessage) {
	for _, p := range peers {
		go func(peer string) {
			for i := 0; i < 3; i++ {
				if Send(peer, msg) == nil {
					return
				}
				time.Sleep(50 * time.Millisecond)
			}
		}(p)
	}
}
