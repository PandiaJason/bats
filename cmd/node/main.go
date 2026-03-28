package main

import (
	"fmt"
	"os"
	"strings"

	"bats/internal/node"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Println("usage: node <id> <port>")
		return
	}

	id := os.Args[1]
	port := os.Args[2]

	peersEnv := os.Getenv("PEERS")
	var peers []string
	if peersEnv != "" {
		if peersEnv == "NONE" {
			peers = []string{}
		} else {
			peers = strings.Split(peersEnv, ",")
		}
	} else {
		peers = []string{
			"localhost:8001",
			"localhost:8002",
			"localhost:8003",
			"localhost:8004",
		}
	}

	n := node.NewNode(id, port, peers)
	n.Start(port)
}
