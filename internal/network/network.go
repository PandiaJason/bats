package network

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"os"
	"time"

	"bats-cluster/internal/types"
	"google.golang.org/protobuf/proto"
)

var client *http.Client

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
		Timeout: 5 * time.Second,
	}
}

func Send(addr string, msg *types.ConsensusMessage) error {
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

func Broadcast(peers []string, msg *types.ConsensusMessage) {
	for _, p := range peers {
		go func(peer string) {
			for i := 0; i < 3; i++ {
				if Send(peer, msg) == nil {
					return
				}
				time.Sleep(100 * time.Millisecond)
			}
		}(p)
	}
}
