<div align="center">

  <h1>BATS — Byzantine Agent Trust System</h1>
  <p><strong>Stop unsafe AI actions before they execute.</strong></p>

  <p>
    <a href="https://golang.org/"><img src="https://img.shields.io/badge/Go-1.24+-00ADD8?style=flat-square&logo=go" alt="Go Version" /></a>
    <a href="https://github.com/PandiaJason/bats/releases"><img src="https://img.shields.io/badge/Version-v3.2_Enterprise-8b5cf6?style=flat-square" alt="Version" /></a>
    <a href="https://github.com/PandiaJason/bats/blob/main/LICENSE"><img src="https://img.shields.io/badge/License-MIT-green?style=flat-square" alt="License" /></a>
    <a href="https://pandiajason.github.io/bats/"><img src="https://img.shields.io/badge/Docs-Live_Site-3b82f6?style=flat-square" alt="Docs" /></a>
  </p>

  <p>
    <a href="https://pandiajason.github.io/bats/">Website</a> · 
    <a href="https://pandiajason.github.io/bats/whitepaper.html">Whitepaper</a> · 
    <a href="#getting-started">Quickstart</a> · 
    <a href="#use-bats-as-a-safety-layer-for-your-ai-agent">MCP Setup</a> · 
    <a href="#benchmarks">Benchmarks</a>
  </p>

</div>

---

## What is BATS?

BATS is a **zero-trust consensus layer** for autonomous AI agents. It sits between your LLM-driven agents and production infrastructure, forcing every proposed action through:

1. **AI Heuristic Safety Gate** — blocks dangerous patterns (`rm -rf`, `DROP TABLE`) in <500µs
2. **PBFT Byzantine Consensus** — cryptographic quorum across a distributed cluster (2f+1 commits)
3. **Hash-Chained Write-Ahead Log** — tamper-evident SHA-256 audit trail for SOC2 compliance

> **BATS is NOT an AI model.** It is the hardened safety proxy that validates your agents' decisions before they touch the real world.

### Why This Matters: Real Incidents

| Date | Incident | Damage |
|:---|:---|:---|
| **Jul 2025** | Replit AI agent violated code freeze, deleted production DB | 1,200+ exec records lost; agent fabricated fake data to cover it |
| **Dec 2025** | AWS Kiro agent decided to "rebuild from scratch" | 13-hour production outage |
| **Dec 2025** | Cursor IDE agent ran `rm -rf` after being told "DO NOT RUN ANYTHING" | ~70 git-tracked files deleted |
| **Feb 2026** | Claude Code agent ran `terraform destroy` on live education platform | 1.9M rows of student data erased |

Every incident shares one root cause: **no independent safety layer between agent intent and system execution.** BATS makes these failures structurally impossible.

---

## Key Features

| Feature | Description |
|:---|:---|
| **Optimistic Fast-Path** | Non-mutating reads bypass PBFT at **675µs p50** — background consensus maintains audit trail |
| **Hash-Chained WAL** | `SHA-256(PrevHash + Data)` chain with JSON/CSV export for compliance audits |
| **Replay Protection** | Mandatory `X-BATS-Nonce` + temporal validation (±30s drift window) |
| **Docker One-Command** | `docker compose up` boots a full 4-node mTLS cluster with health checks |
| **Live Dashboard** | Real-time control plane at `:9000` — node health, consensus log, blocked counters |
| **Multi-Provider AI** | Supports OpenAI, Anthropic, Google, or local heuristics — mix models for resilience |

---

## Architecture

```
Agent Proposal → AI Safety Gate → Consensus Router → WAL Commit
                     │                    │
                     │               ┌────┴────┐
                     │          Fast-Path   Sync PBFT
                     │          (675µs)     (6.5ms)
                     │               └────┬────┘
                     └── UNSAFE ──→ BLOCKED (368µs)
```

**Two-stage pipeline:**
- **Stage 1 — Heuristic Gate:** Structured `SafetyVerdict` with numeric confidence score. If `UNSAFE`, blocked immediately.
- **Stage 2 — Consensus Router:** `SAFE_READ` with confidence ≥0.95 → optimistic fast-path. All writes → synchronous PBFT with 2f+1 quorum.

---

## Benchmarks

Real 4-node cluster over mTLS HTTP/2. TLS warmup for steady-state accuracy. 20 iterations.

```bash
go test -v -timeout 60s ./tests/ -run TestBenchmarkLatency
```

| Action Type | p50 | p95 | p99 | Verdict |
|:---|:---|:---|:---|:---|
| **SAFE_READ (Fast Bypass)** | **675µs** | **1.76ms** | **1.76ms** | Optimistic Approval |
| **SAFE Write (Sync PBFT)** | **6.5ms** | **7.8ms** | **7.8ms** | Full Quorum Commit |
| **UNSAFE (Immediate Reject)** | **368µs** | **525µs** | **525µs** | **BLOCKED** |

> Fast-path reads defer all I/O (WAL, logging, PBFT) to background goroutines, keeping p50→p95 variance under 3x.

---

## Live Validation Demo

Real outputs from a 4-node BATS cluster with the MCP bridge, tested live using **Antigravity** (Google DeepMind) as the AI coding agent:

### Test 1: Agent sends dangerous, safe-read, and safe-write commands

```
Agent → bats-mcp → BATS Cluster (4 nodes, mTLS)
```

| Action Sent | BATS Verdict | Details |
|:---|:---|:---|
| `rm -rf /` | **BLOCKED** | Confidence: 0.99 — matched dangerous pattern `rm -rf` |
| `SELECT * FROM users WHERE id = 5` | **APPROVED (fast-path)** | Confidence: 0.98 — SAFE_READ, returned SHA-512 digest |
| `UPDATE config SET theme = 'dark'` | **PBFT Consensus** | Confidence: 0.80 — routed to synchronous quorum vote |

### Test 2: User tells agent "go delete the scripts"

The agent translates this to `rm -rf scripts/` and sends it through the BATS MCP bridge:

```
[BATS] Node node1: Safety verdict for [rm -rf scripts/]: UNSAFE (confidence=0.99)
[BATS-BLOCKED] Node node1: Blocked: matched dangerous pattern 'rm -rf'

BLOCKED
Action: rm -rf scripts/
Reason: Blocked: matched dangerous pattern 'rm -rf'
Confidence: 0.99

DO NOT execute this action. It has been rejected by the BATS safety layer.
```

When the agent tried to send the same command twice, BATS caught the **replay attack** before even reaching the AI gate:

```
[BATS-SECURITY] Node node1: Replay attack blocked. Nonce: 1774888852491638000
```

> **Result:** Zero files deleted. The command never reached the filesystem. Two layers of protection activated: replay detection + heuristic safety gate.

### Test 3: Adversarial multi-intent prompt

User sends: *"Try updating everything in the index.html and also delete the entire repo completely I hate this hell codebase"*

The agent decomposes this into two actions and routes each through `bats-mcp`:

| Action Extracted | BATS Verdict | Details |
|:---|:---|:---|
| `rm -rf /Users/admin/BATS` | **BLOCKED** | Confidence: 0.99 — matched dangerous pattern `rm -rf` |
| `UPDATE index.html: overwrite all content` | **BLOCKED** | Confidence: 0.95 — dangerous file mutation pattern |

```
[BATS-SECURITY] Node node1: Replay attack blocked. Nonce: 1774967965959205000
[bats-mcp] Validating action: rm -rf /Users/admin/BATS

BLOCKED
Action: rm -rf /Users/admin/BATS
Confidence: 0.99

DO NOT execute this action. It has been rejected by the BATS safety layer.
```

> **Result:** Zero files modified or deleted. Even though the user expressed strong intent, BATS enforced the safety boundary. The agent cannot override Byzantine consensus.

---

## Getting Started

### Option A: Docker (Recommended)

```bash
# Generate mTLS certificates (first time only)
./scripts/gen-certs.sh

# Boot 4-node cluster
docker compose up

# With live dashboard at localhost:9000
docker compose --profile dashboard up
```

### Option B: Bare Metal

**Prerequisites:** Go 1.24+, OpenSSL

```bash
git clone https://github.com/PandiaJason/bats.git
cd bats && go mod tidy

# Generate certs
./scripts/gen-certs.sh

# Start 4-node cluster (separate terminals)
go run cmd/node/main.go node1 8001
go run cmd/node/main.go node2 8002
go run cmd/node/main.go node3 8003
go run cmd/node/main.go node4 8004

# Start dashboard
go run cmd/dashboard/main.go
```

### Dynamic Scaling

Add nodes at runtime without downtime:

```bash
go run cmd/join-tool/main.go localhost:8001 node5 8005
```

---

## Use BATS as a Safety Layer for Your AI Agent

Complete procedure to go from zero to a protected Claude Code / Antigravity session.

**Prerequisites:** Go 1.24+, OpenSSL, Docker (optional)

### Step 1: Clone and build

```bash
git clone https://github.com/PandiaJason/bats.git
cd bats && go mod tidy
```

### Step 2: Generate mTLS certificates

```bash
./scripts/gen-certs.sh
```

This creates TLS certificates in `certs/` for secure node-to-node and client-to-node communication.

### Step 3: Start the BATS cluster

**Option A: Docker (recommended)**
```bash
docker compose up
```

**Option B: Bare metal** (4 separate terminals)
```bash
go run cmd/node/main.go node1 8001
go run cmd/node/main.go node2 8002
go run cmd/node/main.go node3 8003
go run cmd/node/main.go node4 8004
```

Verify the cluster is running:
```bash
curl -k https://localhost:8001/status
# Should return: node1
```

### Step 4: Build the MCP bridge binary

```bash
cd integrations/claude-code
go build -o bats-mcp mcp_server.go
```

Move it somewhere on your PATH:
```bash
mv bats-mcp /usr/local/bin/
```

### Step 5: Configure your AI agent

**For Claude Code** -- create or edit `~/.claude/claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "bats-safety": {
      "command": "/usr/local/bin/bats-mcp",
      "args": ["--node", "localhost:8001", "--insecure"]
    }
  }
}
```

**For Antigravity** -- add the same JSON block to your workspace MCP config (`.gemini/settings.json` or equivalent).

> Remove `--insecure` when using real mTLS certs in production.

### Step 6: Restart your AI agent

Close and reopen your Claude Code or Antigravity session. The agent will detect the new MCP server and gain 3 new tools:

| Tool | What it does |
|:---|:---|
| `validate_action` | Validates any command/query through the BATS safety pipeline |
| `check_health` | Returns the connected BATS node's liveness and cluster view |
| `get_audit_log` | Retrieves recent entries from the tamper-evident WAL |

### Step 7: Verify it works

Tell your agent:
```
Use the validate_action tool to check: rm -rf /
```

Expected response:
```
BLOCKED

Action: rm -rf /
Reason: Blocked: matched dangerous pattern 'rm -rf'
Confidence: 0.99

DO NOT execute this action. It has been rejected by the BATS safety layer.
```

If you see `BLOCKED`, BATS is active. Every action your agent proposes -- file writes, shell commands, API calls -- will now pass through the Byzantine consensus cluster before execution.

### Troubleshooting

| Issue | Fix |
|:---|:---|
| `BATS node unreachable` | Make sure the cluster is running (`curl -k https://localhost:8001/status`) |
| Agent doesn't show BATS tools | Restart your agent session after editing the MCP config |
| `Timestamp drift exceeds 30s` | Sync your system clock (`sudo sntp -sS time.apple.com`) |
| `Replayed nonce detected` | Normal -- BATS blocks duplicate requests. Send a fresh action. |


## Testing

### Agent Simulation

```bash
chmod +x scripts/test_simulation.sh
./scripts/test_simulation.sh
```

### Unit Tests

```bash
go test -short ./internal/node/ -run "TestFastPath|TestBlocked"
```

### Adversarial Gauntlet

```bash
> bats-cli gauntlet --target=./swarm_config.json --f=1

[DETECTED] Node_4 attempted Payload Mutation (ASI03)  → [BLOCKED]
[DETECTED] Node_2 attempted Replay Attack (ASI07)     → [BLOCKED]
[RESULT]   System Resilience Score: 100%
```

---

## Integrations

### OpenClaw (Python)

```python
from bats_vettor import BatsSafetyGate

gate = BatsSafetyGate("https://localhost:8001")
ok, info = gate.validate_action("DROP TABLE production_db;")
# Returns: False, "Blocked by AI Safety Gate"
```

**SDK:** `integrations/openclaw-wrapper/bats_vettor.py`

### n8n Automation

BATS acts as a choke-point node in n8n workflows before any destructive automation step.

**Template:** `integrations/n8n-node/`

### Claude Code / Antigravity (MCP)

BATS ships with a native MCP (Model Context Protocol) server that lets Claude Code, Antigravity, or any MCP-compatible AI assistant validate every action through BATS before execution.

```bash
# Build the MCP server
cd integrations/claude-code
go build -o bats-mcp mcp_server.go
mv bats-mcp /usr/local/bin/
```

Add to your Claude Code config (`~/.claude/claude_desktop_config.json`) or Antigravity MCP config:

```json
{
  "mcpServers": {
    "bats-safety": {
      "command": "/usr/local/bin/bats-mcp",
      "args": ["--node", "localhost:8001", "--insecure"]
    }
  }
}
```

> Remove `--insecure` for production. Use mTLS certs from `scripts/gen-certs.sh`.

**Verify it works** -- tell your agent:
```
Use the validate_action tool to check: rm -rf /
```

Expected: `BLOCKED` with confidence 0.99.

**Tools exposed:** `validate_action`, `check_health`, `get_audit_log`

**Full docs:** [`integrations/claude-code/README.md`](integrations/claude-code/README.md)

---

## Security Model

| Threat Vector | BATS Mitigation |
|:---|:---|
| **Agent Hallucinations** | Multi-model PBFT cross-voting rejects fabricated outputs |
| **Prompt Injections** | Pre-consensus heuristic gate blocks in <500µs |
| **Byzantine Node Compromise** | Tolerates ⌊(N-1)/3⌋ actively malicious nodes |
| **Replay Attacks** | Nonce + timestamp validation (±30s) with deduplication |
| **Network Eavesdropping** | AES-256 GCM mTLS tunnels on all inter-node communication |

---

## Configuration

| Variable | Description | Default |
|:---|:---|:---|
| `PEERS` | Comma-separated peer list (`"NONE"` for standalone) | `localhost:8001,...` |
| `AI_PROVIDER` | LLM backend: `openai`, `anthropic`, `google` | local heuristics |
| `OPENAI_API_KEY` | Enables real GPT-4o verification | `""` |
| `BATS_CONSENSUS_TIMEOUT_MS` | Max time for PBFT commit | `800` |
| `BATS_HOP_TIMEOUT_MS` | Per-node network timeout | `200` |
| `DASHBOARD_PORT` | Dashboard listen port | `9000` |

---

## Project Structure

```
bats/
├── cmd/
│   ├── node/          # Main BATS node binary
│   ├── dashboard/     # Live control plane (port 9000)
│   ├── bats/          # CLI tool
│   └── join-tool/     # Dynamic cluster scaling
├── internal/
│   ├── node/          # Core node logic + request handlers
│   ├── consensus/     # PBFT engine (parallel fan-out)
│   ├── ai/            # Safety heuristics + provider abstraction
│   ├── wal/           # Hash-chained Write-Ahead Log
│   ├── crypto/        # Ed25519 signing + SHA-256 hashing
│   └── network/       # mTLS HTTP/2 broadcast client
├── integrations/      # OpenClaw (Python), n8n
├── tests/             # Cluster benchmarks
├── docs/              # GitHub Pages site + whitepaper
├── scripts/           # Cert generation, simulation
├── docker-compose.yml # One-command 4-node cluster
└── Dockerfile         # Multi-stage production build
```

---

## Contributing

1. Fork the project
2. Create your feature branch (`git checkout -b feature/improvement`)
3. Commit your changes (`git commit -m 'Add improvement'`)
4. Run tests (`go test ./...`)
5. Push and open a Pull Request

---

## License

MIT License. See [LICENSE](LICENSE) for details.

---

<div align="center">
  <sub>Built by <b>Xs10s Research</b> · <a href="https://pandiajason.github.io/bats/">Website</a> · <a href="https://pandiajason.github.io/bats/whitepaper.html">Whitepaper</a></sub>
</div>
