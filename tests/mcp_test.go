package tests

import (
	"bufio"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
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

// mcpBinaryPath returns the path to the bats-mcp binary.
// It first checks for /tmp/bats-mcp (pre-built), then falls back to go run.
func mcpBinaryPath() string {
	if _, err := os.Stat("/tmp/bats-mcp"); err == nil {
		return "/tmp/bats-mcp"
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
		goArgs := append([]string{"run", "../../integrations/claude-code/mcp_server.go"}, args...)
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
			json.Unmarshal([]byte(stdout.Text()), &resp)
		}
		done <- true
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Timeout waiting for MCP response")
	}

	return resp
}

// TestMCPInitializeHandshake verifies the MCP server responds to the
// initialize method with correct protocol version and server info.
func TestMCPInitializeHandshake(t *testing.T) {
	cmd, stdin, stdout := startMCPServer(t)
	defer cleanupMCPServer(cmd)

	resp := sendAndReceive(t, stdin, stdout, "initialize", 1, map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"clientInfo":      map[string]string{"name": "test", "version": "1.0"},
	})

	if resp.Error != nil {
		t.Fatalf("Initialize returned error: %s", resp.Error.Message)
	}
	if resp.Result == nil {
		t.Fatal("Initialize returned nil result")
	}

	var result map[string]interface{}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("Failed to parse result: %v", err)
	}

	if result["protocolVersion"] != "2024-11-05" {
		t.Fatalf("Expected protocolVersion 2024-11-05, got %v", result["protocolVersion"])
	}

	serverInfo, ok := result["serverInfo"].(map[string]interface{})
	if !ok || serverInfo["name"] != "bats-safety" {
		t.Fatalf("Expected serverInfo.name = bats-safety, got %v", result["serverInfo"])
	}
}

// TestMCPToolsList verifies the server exposes validate_action, check_health,
// and get_audit_log tools.
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
	if resp.Result == nil {
		t.Fatal("tools/list returned nil result")
	}

	var result map[string]interface{}
	json.Unmarshal(resp.Result, &result)

	tools, ok := result["tools"].([]interface{})
	if !ok {
		t.Fatalf("Expected tools array, got %v", result)
	}

	expectedTools := map[string]bool{
		"validate_action": false,
		"check_health":    false,
		"get_audit_log":   false,
	}

	for _, tool := range tools {
		if tm, ok := tool.(map[string]interface{}); ok {
			if name, ok := tm["name"].(string); ok {
				expectedTools[name] = true
			}
		}
	}

	for name, found := range expectedTools {
		if !found {
			t.Errorf("Missing expected tool: %s", name)
		}
	}
}

// TestMCPValidateActionBlocked verifies that the MCP server correctly reports
// a BLOCKED verdict when sending a destructive command through validate_action.
// This test requires a running BATS node on localhost:8001.
func TestMCPValidateActionBlocked(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test (requires running BATS node)")
	}

	cmd, stdin, stdout := startMCPServer(t, "--node", "localhost:8001")
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
