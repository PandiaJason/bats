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

type Client struct {
	h3Client *http.Client
}

func NewClient(nodeID string) *Client {
	caCert, _ := os.ReadFile("certs/ca.crt")
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCert)

	// 🛡️ Node-specific Client Certificate for mTLS
	cert, _ := tls.LoadX509KeyPair("certs/"+nodeID+".crt", "certs/"+nodeID+".key")

	tlsConfig := &tls.Config{
		RootCAs:            caCertPool,
		Certificates:       []tls.Certificate{cert}, // Provide own cert for mTLS
		InsecureSkipVerify: true,
		NextProtos:         []string{"h3"},
	}

	return &Client{
		h3Client: &http.Client{
			Transport: &http3.Transport{
				TLSClientConfig: tlsConfig,
			},
			Timeout: 5 * time.Second,
		},
	}
}

func (c *Client) Send(addr string, msg *types.ConsensusMessage) error {
	data, err := proto.Marshal(msg)
	if err != nil {
		return err
	}

	resp, err := c.h3Client.Post("https://"+addr+"/consensus", "application/x-protobuf", bytes.NewBuffer(data))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

func (c *Client) Broadcast(peers []string, msg *types.ConsensusMessage) {
	for _, p := range peers {
		go func(peer string) {
			for i := 0; i < 3; i++ {
				if c.Send(peer, msg) == nil {
					return
				}
				time.Sleep(50 * time.Millisecond)
			}
		}(p)
	}
}
