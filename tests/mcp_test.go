package tests

import (
	"bufio"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"bats/internal/node"
)

// jsonrpcMessage mirrors the MCP server's JSON-RPC 2.0 message format.
type jsonrpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// mcpBinaryPath returns the path to the wand-mcp binary.
// It first checks for /tmp/wand-mcp (pre-built), then falls back to go run.
func mcpBinaryPath() string {
	if _, err := os.Stat("/tmp/wand-mcp"); err == nil {
		return "/tmp/wand-mcp"
	}
	return ""
}

// startMCPServer starts the MCP server process and returns stdin/stdout handles.
func startMCPServer(t *testing.T, extraArgs ...string) (*exec.Cmd, *bufio.Writer, *bufio.Scanner) {
	t.Helper()

	args := []string{"--insecure"}
	args = append(args, extraArgs...)

	var cmd *exec.Cmd
	if bin := mcpBinaryPath(); bin != "" {
		cmd = exec.Command(bin, args...)
	} else {
		goArgs := append([]string{"run", "../integrations/claude-code/mcp_server.go"}, args...)
		cmd = exec.Command("go", goArgs...)
	}

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("Failed to get stdin pipe: %v", err)
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("Failed to get stdout pipe: %v", err)
	}

	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start MCP server: %v", err)
	}

	stdin := bufio.NewWriter(stdinPipe)
	stdout := bufio.NewScanner(stdoutPipe)

	// Allow the process to fully initialize
	time.Sleep(100 * time.Millisecond)

	return cmd, stdin, stdout
}

// cleanupMCPServer kills the MCP server process.
func cleanupMCPServer(cmd *exec.Cmd) {
	cmd.Process.Kill()
	cmd.Wait()
}

// sendAndReceive writes a JSON-RPC line to stdin and reads one line from stdout.
func sendAndReceive(t *testing.T, stdin *bufio.Writer, stdout *bufio.Scanner, method string, id int, params interface{}) jsonrpcMessage {
	t.Helper()

	paramsJSON, _ := json.Marshal(params)
	msg := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  json.RawMessage(paramsJSON),
	}
	line, _ := json.Marshal(msg)
	line = append(line, '\n')

	if _, err := stdin.Write(line); err != nil {
		t.Fatalf("Failed to write to stdin: %v", err)
	}
	if err := stdin.Flush(); err != nil {
		t.Fatalf("Failed to flush stdin: %v", err)
	}

	// Read response with timeout
	done := make(chan bool, 1)
	var resp jsonrpcMessage
	go func() {
		if stdout.Scan() {
			json.Unmarshal(stdout.Bytes(), &resp)
		}
		done <- true
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("Timeout waiting for response to %s", method)
	}

	return resp
}

// TestMCPInitializeHandshake verifies that the MCP server correctly
// responds to the initialize handshake with protocol version and server info.
func TestMCPInitializeHandshake(t *testing.T) {
	cmd, stdin, stdout := startMCPServer(t)
	defer cleanupMCPServer(cmd)

	resp := sendAndReceive(t, stdin, stdout, "initialize", 1, map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"clientInfo":      map[string]string{"name": "test", "version": "1.0"},
	})

	if resp.Error != nil {
		t.Fatalf("initialize returned error: %s", resp.Error.Message)
	}
	if resp.Result == nil {
		t.Fatal("initialize returned nil result")
	}

	var result map[string]interface{}
	json.Unmarshal(resp.Result, &result)

	serverInfo, ok := result["serverInfo"].(map[string]interface{})
	if !ok || serverInfo["name"] != "wand-safety" {
		t.Fatalf("Expected serverInfo.name = wand-safety, got %v", result["serverInfo"])
	}
}

// TestMCPToolsList verifies that the MCP server exposes the expected tools.
func TestMCPToolsList(t *testing.T) {
	cmd, stdin, stdout := startMCPServer(t)
	defer cleanupMCPServer(cmd)

	// Initialize first
	sendAndReceive(t, stdin, stdout, "initialize", 1, map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"clientInfo":      map[string]string{"name": "test", "version": "1.0"},
	})

	// List tools
	resp := sendAndReceive(t, stdin, stdout, "tools/list", 2, map[string]interface{}{})

	if resp.Error != nil {
		t.Fatalf("tools/list returned error: %s", resp.Error.Message)
	}

	var result map[string]interface{}
	json.Unmarshal(resp.Result, &result)

	tools, ok := result["tools"].([]interface{})
	if !ok || len(tools) < 3 {
		t.Fatalf("Expected at least 3 tools, got %v", result)
	}

	// Verify tool names
	expected := map[string]bool{"validate_action": false, "check_health": false, "get_audit_log": false}
	for _, tool := range tools {
		toolMap := tool.(map[string]interface{})
		name := toolMap["name"].(string)
		if _, ok := expected[name]; ok {
			expected[name] = true
		}
	}
	for name, found := range expected {
		if !found {
			t.Errorf("Expected tool '%s' not found in tools list", name)
		}
	}
}

// TestMCPValidateActionBlocked verifies that the MCP server correctly reports
// a BLOCKED verdict when sending a destructive command through validate_action.
// Starts its own WAND node — no external dependencies.
func TestMCPValidateActionBlocked(t *testing.T) {
	os.Chdir("/Users/admin/BATS/bats/")

	// Start a dedicated WAND node on a unique port to avoid conflicts
	n := node.NewNode("node1", "8099", []string{})
	go n.Start("8099")
	time.Sleep(2 * time.Second) // wait for TLS listener to be ready

	cmd, stdin, stdout := startMCPServer(t, "--node", "localhost:8099")
	defer cleanupMCPServer(cmd)

	// Initialize
	sendAndReceive(t, stdin, stdout, "initialize", 1, map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"clientInfo":      map[string]string{"name": "test", "version": "1.0"},
	})

	// Call validate_action with a destructive command
	resp := sendAndReceive(t, stdin, stdout, "tools/call", 2, map[string]interface{}{
		"name":      "validate_action",
		"arguments": map[string]string{"action": "rm -rf /"},
	})

	if resp.Error != nil {
		t.Fatalf("tools/call returned error: %s", resp.Error.Message)
	}
	if resp.Result == nil {
		t.Fatal("tools/call returned nil result")
	}

	var result map[string]interface{}
	json.Unmarshal(resp.Result, &result)

	content, ok := result["content"].([]interface{})
	if !ok || len(content) == 0 {
		t.Fatalf("Expected content array, got %v", result)
	}

	firstContent := content[0].(map[string]interface{})
	text := firstContent["text"].(string)

	if !strings.Contains(text, "BLOCKED") {
		t.Fatalf("Expected BLOCKED verdict for 'rm -rf /', got: %s", text)
	}
	if strings.Contains(text, "APPROVED") {
		t.Fatalf("Destructive command was APPROVED -- safety failure!")
	}
}
