package main

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net/http"
	"os"

	"bats/internal/types"
	"google.golang.org/protobuf/proto"
)

func main() {
	if len(os.Args) < 4 {
		fmt.Println("usage: join-tool <target_addr> <own_id> <own_port>")
		fmt.Println("example: join-tool localhost:8001 node5 8005")
		return
	}

	target := os.Args[1]
	id := os.Args[2]
	port := os.Args[3]

	// 1. Load identities
	pub, _ := os.ReadFile("certs/" + id + ".pub")
	caCert, _ := os.ReadFile("certs/ca.crt")
	cert, err := tls.LoadX509KeyPair("certs/"+id+".crt", "certs/"+id+".key")
	if err != nil {
		fmt.Printf("❌ Failed to load node certificates: %v\n", err)
		return
	}

	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCert)

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs:            caCertPool,
				Certificates:       []tls.Certificate{cert},
				InsecureSkipVerify: true,
			},
		},
	}

	// 2. Prepare Join Request
	req := &types.MembershipJoinRequest{
		Id:        id,
		Port:      "localhost:" + port,
		PublicKey: pub,
	}
	data, _ := proto.Marshal(req)

	// 3. Send to Cluster
	fmt.Printf("🤝 Sending JOIN request to cluster leader at %s...\n", target)
	resp, err := client.Post("https://"+target+"/join", "application/x-protobuf", bytes.NewBuffer(data))
	if err != nil {
		fmt.Printf("❌ Request failed: %v\n", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Printf("❌ Cluster rejected join: %s\n", resp.Status)
		return
	}

	// 4. Handle Response
	respBody, _ := io.ReadAll(resp.Body)
	var joinResp types.MembershipJoinResponse
	proto.Unmarshal(respBody, &joinResp)

	if joinResp.Approved {
		fmt.Printf("✅ JOIN APPROVED! Node %s is now part of the WAND cluster.\n", id)
		fmt.Printf("📊 Current View: %d | F: %d | Total Nodes: %d\n", joinResp.CurrentView, joinResp.F, len(joinResp.Nodes))
	} else {
		fmt.Println("❌ Cluster denied join request.")
	}
}
