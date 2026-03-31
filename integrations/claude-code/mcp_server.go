// Package main implements a Model Context Protocol (MCP) server that bridges
// Claude Code, Antigravity, or any MCP-compatible AI coding assistant to BATS.
//
// It reads JSON-RPC 2.0 messages from stdin and writes responses to stdout,
// forwarding safety validation requests to a running BATS node.
//
// Usage:
//
//	bats-mcp --node localhost:8001
//	bats-mcp --node localhost:8001 --insecure
package main

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// ─── JSON-RPC 2.0 Types ───

type jsonrpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  interface{}     `json:"result,omitempty"`
	Error   *jsonrpcError   `json:"error,omitempty"`
}

type jsonrpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// ─── MCP Protocol Types ───

type initializeParams struct {
	ProtocolVersion string `json:"protocolVersion"`
	ClientInfo      struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"clientInfo"`
}

type serverInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type initializeResult struct {
	ProtocolVersion string       `json:"protocolVersion"`
	ServerInfo      serverInfo   `json:"serverInfo"`
	Capabilities    capabilities `json:"capabilities"`
}

type capabilities struct {
	Tools *toolsCap `json:"tools,omitempty"`
}

type toolsCap struct {
	ListChanged bool `json:"listChanged"`
}

type tool struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema inputSchema `json:"inputSchema"`
}

type inputSchema struct {
	Type       string              `json:"type"`
	Properties map[string]property `json:"properties"`
	Required   []string            `json:"required,omitempty"`
}

type property struct {
	Type        string `json:"type"`
	Description string `json:"description"`
}

type toolsListResult struct {
	Tools []tool `json:"tools"`
}

type callToolParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type textContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type callToolResult struct {
	Content []textContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

// ─── BATS Client ───

type batsClient struct {
	nodeAddr   string
	httpClient *http.Client
}

func newBATSClient(addr string, insecure bool) *batsClient {
	tlsCfg := &tls.Config{}
	if insecure {
		tlsCfg.InsecureSkipVerify = true
	}
	return &batsClient{
		nodeAddr: addr,
		httpClient: &http.Client{
			Timeout:   5 * time.Second,
			Transport: &http.Transport{TLSClientConfig: tlsCfg},
		},
	}
}

// validate sends an action to the BATS /validate endpoint.
func (c *batsClient) validate(action string) (map[string]interface{}, error) {
	body, _ := json.Marshal(map[string]string{"action": action})
	req, err := http.NewRequest("POST", "https://"+c.nodeAddr+"/validate", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-BATS-Nonce", fmt.Sprintf("%d-%d", time.Now().UnixNano(), time.Now().UnixNano()%1000000))
	req.Header.Set("X-BATS-Timestamp", fmt.Sprintf("%d", time.Now().Unix()))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("BATS node unreachable at %s: %v", c.nodeAddr, err)
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("invalid response from BATS: %v", err)
	}
	return result, nil
}

// health queries the BATS /status endpoint.
func (c *batsClient) health() (map[string]interface{}, error) {
	resp, err := c.httpClient.Get("https://" + c.nodeAddr + "/status")
	if err != nil {
		return nil, fmt.Errorf("BATS node unreachable: %v", err)
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	// The /status endpoint returns protobuf, so we'll return a simple JSON status
	return map[string]interface{}{
		"node":   c.nodeAddr,
		"alive":  resp.StatusCode == 200,
		"bytes":  len(data),
		"status": resp.Status,
	}, nil
}

// walEntries queries the BATS /wal endpoint.
func (c *batsClient) walEntries(limit int) (interface{}, error) {
	url := fmt.Sprintf("https://%s/wal?format=json&limit=%d", c.nodeAddr, limit)
	resp, err := c.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("BATS WAL unreachable: %v", err)
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	var entries interface{}
	if err := json.Unmarshal(data, &entries); err != nil {
		// If WAL returns non-JSON, wrap as string
		return string(data), nil
	}
	return entries, nil
}

// ─── MCP Server ───

type mcpServer struct {
	bats    *batsClient
	scanner *bufio.Scanner
	writer  *json.Encoder
	stderr  *os.File
}

func newMCPServer(bats *batsClient) *mcpServer {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 1MB buffer
	return &mcpServer{
		bats:    bats,
		scanner: scanner,
		writer:  json.NewEncoder(os.Stdout),
		stderr:  os.Stderr,
	}
}

func (s *mcpServer) log(format string, args ...interface{}) {
	fmt.Fprintf(s.stderr, "[bats-mcp] "+format+"\n", args...)
}

func (s *mcpServer) respond(id json.RawMessage, result interface{}) {
	msg := jsonrpcMessage{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	}
	s.writer.Encode(msg)
}

func (s *mcpServer) respondError(id json.RawMessage, code int, message string) {
	msg := jsonrpcMessage{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &jsonrpcError{Code: code, Message: message},
	}
	s.writer.Encode(msg)
}

func (s *mcpServer) run() {
	s.log("Starting BATS MCP server (node: %s)", s.bats.nodeAddr)

	for s.scanner.Scan() {
		line := strings.TrimSpace(s.scanner.Text())
		if line == "" {
			continue
		}

		var msg jsonrpcMessage
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			s.log("Parse error: %v", err)
			continue
		}

		s.handleMessage(&msg)
	}
}

func (s *mcpServer) handleMessage(msg *jsonrpcMessage) {
	switch msg.Method {

	// ─── Lifecycle ───
	case "initialize":
		s.log("Client connected, negotiating capabilities")
		s.respond(msg.ID, initializeResult{
			ProtocolVersion: "2024-11-05",
			ServerInfo: serverInfo{
				Name:    "bats-safety",
				Version: "3.2.0",
			},
			Capabilities: capabilities{
				Tools: &toolsCap{ListChanged: false},
			},
		})

	case "notifications/initialized":
		s.log("Session initialized")
		// No response needed for notifications

	case "ping":
		s.respond(msg.ID, map[string]string{})

	// ─── Tools ───
	case "tools/list":
		s.respond(msg.ID, toolsListResult{
			Tools: []tool{
				{
					Name:        "validate_action",
					Description: "Send a proposed action to BATS for safety validation. Returns approved/blocked with confidence score and reasoning. Use this BEFORE executing any potentially dangerous command, file operation, or API call.",
					InputSchema: inputSchema{
						Type: "object",
						Properties: map[string]property{
							"action": {
								Type:        "string",
								Description: "The action string to validate (e.g., 'rm -rf /tmp/data', 'DROP TABLE users', 'curl https://internal-api/admin')",
							},
							"context": {
								Type:        "string",
								Description: "Optional context about why this action is being performed",
							},
						},
						Required: []string{"action"},
					},
				},
				{
					Name:        "check_health",
					Description: "Check the health and status of the connected BATS safety node. Returns node ID, liveness, and cluster view.",
					InputSchema: inputSchema{
						Type:       "object",
						Properties: map[string]property{},
					},
				},
				{
					Name:        "get_audit_log",
					Description: "Retrieve recent entries from the BATS tamper-evident Write-Ahead Log (WAL). Shows approved, blocked, and committed actions with hash chain integrity.",
					InputSchema: inputSchema{
						Type: "object",
						Properties: map[string]property{
							"limit": {
								Type:        "string",
								Description: "Maximum number of entries to return (default: 20)",
							},
						},
					},
				},
			},
		})

	case "tools/call":
		var params callToolParams
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			s.respondError(msg.ID, -32602, "Invalid params: "+err.Error())
			return
		}
		s.handleToolCall(msg.ID, &params)

	default:
		if msg.ID != nil {
			s.respondError(msg.ID, -32601, "Method not found: "+msg.Method)
		}
	}
}

func (s *mcpServer) handleToolCall(id json.RawMessage, params *callToolParams) {
	switch params.Name {

	case "validate_action":
		var args struct {
			Action  string `json:"action"`
			Context string `json:"context"`
		}
		json.Unmarshal(params.Arguments, &args)

		if args.Action == "" {
			s.respond(id, callToolResult{
				Content: []textContent{{Type: "text", Text: "Error: 'action' argument is required"}},
				IsError: true,
			})
			return
		}

		s.log("Validating action: %s", args.Action)

		result, err := s.bats.validate(args.Action)
		if err != nil {
			s.respond(id, callToolResult{
				Content: []textContent{{Type: "text", Text: fmt.Sprintf("BATS NODE ERROR: %v\n\nThe BATS safety node is unreachable. Action was NOT validated.", err)}},
				IsError: true,
			})
			return
		}

		approved, _ := result["approved"].(bool)
		confidence, _ := result["confidence"].(float64)
		reason, _ := result["reason"].(string)
		digest, _ := result["digest"].(string)
		fastPath, _ := result["fast_path"].(bool)

		var sb strings.Builder
		if approved {
			sb.WriteString("APPROVED")
			if fastPath {
				sb.WriteString(" (fast-path)")
			} else {
				sb.WriteString(" (PBFT consensus)")
			}
			sb.WriteString(fmt.Sprintf("\n\nAction: %s", args.Action))
			sb.WriteString(fmt.Sprintf("\nConfidence: %.2f", confidence))
			sb.WriteString(fmt.Sprintf("\nDigest: %s", digest))
			sb.WriteString("\n\nThis action has been validated by BATS and is safe to execute.")
		} else {
			sb.WriteString("BLOCKED")
			sb.WriteString(fmt.Sprintf("\n\nAction: %s", args.Action))
			sb.WriteString(fmt.Sprintf("\nReason: %s", reason))
			sb.WriteString(fmt.Sprintf("\nConfidence: %.2f", confidence))
			sb.WriteString("\n\nDO NOT execute this action. It has been rejected by the BATS safety layer.")
		}

		s.respond(id, callToolResult{
			Content: []textContent{{Type: "text", Text: sb.String()}},
		})

	case "check_health":
		s.log("Checking BATS node health")
		result, err := s.bats.health()
		if err != nil {
			s.respond(id, callToolResult{
				Content: []textContent{{Type: "text", Text: fmt.Sprintf("BATS node unreachable: %v", err)}},
				IsError: true,
			})
			return
		}

		out, _ := json.MarshalIndent(result, "", "  ")
		s.respond(id, callToolResult{
			Content: []textContent{{Type: "text", Text: fmt.Sprintf("BATS Node Status:\n%s", string(out))}},
		})

	case "get_audit_log":
		var args struct {
			Limit string `json:"limit"`
		}
		json.Unmarshal(params.Arguments, &args)

		limit := 20
		if args.Limit != "" {
			fmt.Sscanf(args.Limit, "%d", &limit)
		}

		s.log("Fetching %d WAL entries", limit)
		entries, err := s.bats.walEntries(limit)
		if err != nil {
			s.respond(id, callToolResult{
				Content: []textContent{{Type: "text", Text: fmt.Sprintf("WAL fetch error: %v", err)}},
				IsError: true,
			})
			return
		}

		out, _ := json.MarshalIndent(entries, "", "  ")
		s.respond(id, callToolResult{
			Content: []textContent{{Type: "text", Text: fmt.Sprintf("BATS Audit Log (last %d entries):\n%s", limit, string(out))}},
		})

	default:
		s.respondError(id, -32602, "Unknown tool: "+params.Name)
	}
}

// ─── Main ───

func main() {
	nodeAddr := flag.String("node", "localhost:8001", "BATS node address (host:port)")
	insecure := flag.Bool("insecure", false, "Skip TLS certificate verification (dev only)")
	flag.Parse()

	client := newBATSClient(*nodeAddr, *insecure)
	server := newMCPServer(client)
	server.run()
}
