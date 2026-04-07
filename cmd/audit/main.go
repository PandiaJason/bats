package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("usage: audit <wal_file_path>")
		return
	}

	path := os.Args[1]
	file, err := os.Open(path)
	if err != nil {
		fmt.Printf("failed to open WAL: %v\n", err)
		return
	}
	defer file.Close()

	fmt.Printf("--- WAND AUDIT REPORT: %s ---\n", path)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		var entry string
		err := json.Unmarshal([]byte(line), &entry)
		if err != nil {
			// Try as plain string if JSON unmarshal fails (for older formats or direct writes)
			entry = line
		}

		if strings.HasPrefix(entry, "COMMITTED:") {
			hash := strings.TrimPrefix(entry, "COMMITTED:")
			fmt.Printf("[ALIVE] ✅ Transaction Committed | Hash: %s\n", hash)
		}
	}

	if err := scanner.Err(); err != nil {
		fmt.Printf("error reading WAL: %v\n", err)
	}
	fmt.Println("--- END OF REPORT ---")
}
