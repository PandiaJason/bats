package network

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"os"
	"time"

	"bats/internal/types"

	"github.com/quic-go/quic-go"
	"google.golang.org/protobuf/proto"
)

var client *http.Client
var tlsConfig *tls.Config

func init() {
	caCert, _ := os.ReadFile("certs/ca.crt")
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCert)

	tlsConfig = &tls.Config{
		RootCAs:            caCertPool,
		InsecureSkipVerify: true,
		NextProtos:         []string{"bats-quic"},
	}

	client = &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs: caCertPool,
			},
		},
		Timeout: 5 * time.Second,
	}
}

func Send(addr string, msg *types.ConsensusMessage) error {
	// ⚡ Attempt QUIC (UDP) first for ultra-low latency
	if err := SendQUIC(addr, msg); err == nil {
		return nil
	}

	// 🛡️ Fallback to HTTPS (TCP) if QUIC fails
	data, err := proto.Marshal(msg)
	if err != nil {
		return err
	}

	resp, err := client.Post("https://"+addr+"/consensus", "application/x-protobuf", bytes.NewBuffer(data))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

func SendQUIC(addr string, msg *types.ConsensusMessage) error {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	conn, err := quic.DialAddr(ctx, addr, tlsConfig, nil)
	if err != nil {
		return err
	}
	defer conn.CloseWithError(0, "")

	stream, err := conn.OpenStreamSync(ctx)
	if err != nil {
		return err
	}
	defer stream.Close()

	data, _ := proto.Marshal(msg)
	_, err = stream.Write(data)
	return err
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
