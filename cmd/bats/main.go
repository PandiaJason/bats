package main

import (
	"fmt"
	"os"
	"os/exec"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("WAND CLI")
		fmt.Println("Usage: wand <command>")
		fmt.Println("Commands:")
		fmt.Println("  start     - Start the cluster via Docker Compose")
		fmt.Println("  stop      - Stop the cluster")
		fmt.Println("  trigger   - Trigger consensus on node1")
		fmt.Println("  ai        - Run AI Agent consensus test")
		fmt.Println("  audit     - Run audit tool on node1 WAL")
		return
	}

	cmd := os.Args[1]
	switch cmd {
	case "start":
		run("docker-compose", "up", "-d", "--build")
	case "stop":
		run("docker-compose", "down")
	case "trigger":
		run("curl", "http://localhost:8001/start")
	case "ai":
		run("go", "run", "./cmd/ai-agent/main.go")
	case "audit":
		run("go", "run", "./cmd/audit/main.go", "wal_node1.log")
	default:
		fmt.Printf("Unknown command: %s\n", cmd)
	}
}

func run(name string, args ...string) {
	c := exec.Command(name, args...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		fmt.Printf("Command failed: %v\n", err)
	}
}
