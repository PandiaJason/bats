<div align="center">
  <img src="https://raw.githubusercontent.com/PandiaJason/bats/main/bats_hero_background.png" alt="BATS Logo" width="600" />

  <h1>BATS (Byzantine Agent Trust System)</h1>
  <p><strong>BATS prevents unsafe AI actions before they happen.</strong></p>

  <p>
    <a href="https://golang.org/"><img src="https://img.shields.io/badge/Go-1.24+-00ADD8?style=flat-square&logo=go" alt="Go Version" /></a>
    <a href="https://github.com/PandiaJason/bats/releases"><img src="https://img.shields.io/badge/Version-v3.0_Enterprise-blue?style=flat-square" alt="Version" /></a>
    <a href="https://github.com/PandiaJason/bats"><img src="https://img.shields.io/badge/Status-Active_Development-orange?style=flat-square" alt="Status" /></a>
    <a href="https://github.com/PandiaJason/bats/blob/main/LICENSE"><img src="https://img.shields.io/badge/License-MIT-green?style=flat-square" alt="License" /></a>
  </p>

  <h3><a href="https://PandiaJason.github.io/bats/">Explore the Live Demo Dashboard</a></h3>

</div>

---

## Why BATS?
In the AI era, the standard approach to autonomous multi-agent orchestration has been either blind trust or frail, centralized API wrappers—leaving critical infrastructure dangerously vulnerable to silent LLM hallucinations and malicious prompt injections. BATS solves this by acting as a decentralized, zero-trust proxy that forces every AI-proposed action to pass a rigid heuristic safety gate and achieve multi-node Byzantine quorum verification before execution, establishing a mathematically sound and tamper-proof new standard for unconditionally secure enterprise AI automation.

---

## Overview

**BATS (Byzantine Agent Trust System)** is a zero-trust, consensus-driven safety layer for AI and autonomous agent workflows. Developed by **Xs10s**, BATS acts as an immutable *Integrity Layer* between non-deterministic AI outputs (e.g., GPT-4, Claude, Gemini) and your critical production infrastructure. 

Instead of blindly trusting an autonomous agent to execute a sensitive action (like mutating a production database or sending funds), BATS enforces that the action must first pass an **AI Heuristic Safety Gate** and then achieve cryptographic quorum via **Practical Byzantine Fault Tolerance (PBFT)** across a distributed cluster.

> [!IMPORTANT]
> **BATS is NOT an AI model.** It is the hardened safety proxy that vets and mathematically verifies your agents' decisions before they touch the real world.

---

## Key Features

- **Heuristic AI Safety Gate**: Instantly blocks malicious intent (e.g., `rm -rf`, DROP TABLE) natively in milliseconds before PBFT processing.
- **Optimistic Fast-Path (<100ms)**: Bypasses synchronous PBFT for deterministic `SAFE_READ` operations, returning authorization natively in ~0.04ms while running clustering in the background.
- **Cryptographic Hash-Chained WAL (SOC2)**: Every ledger entry calculates a strict `SHA-256(PrevHash + Data)`, breaking mathematically if any node suffers unilateral tampering. Exportable to native JSON/CSV.
- **mTLS Zero-Trust & Replay Prevention**: All node communication requires mTLS tunnels alongside strictly validated `X-BATS-Nonce` and temporal envelopes to prevent replay attacks.
- **Elastic Quorum Calculations**: Automatically recalculates Byzantine fault-tolerance thresholds ($F = \lfloor(N-1)/3\rfloor$) as the network scales.
- **Drop-In Integrations**: Native middleware support for **n8n** and **OpenClaw (Python)**.

---

## Architecture Design

At its core, BATS operates as a reverse proxy validation layer. 
1. **Agent Proposal**: An autonomous agent proposes an action via REST/QUIC to the BATS leader.
2. **Safety Heuristics**: The leader locally evaluates the intent of the payload via an embedded LLM Provider heuristic returning a structured `SafetyVerdict` and numeric confidence. If unsafe, it returns a hard `403 Blocked`.
3. **Consensus Routing**:
   - *Fast-Path:* If the payload scores high confidence (>0.95) on non-mutation states (`SAFE_READ`), frontend authorization responds instantly while PBFT runs asynchronously.
   - *Sync-Path:* State-mutating code orchestrates `Pre-Prepare`, `Prepare`, and `Commit` phases across the cluster strictly.
4. **Execution Log**: Once $2f+1$ validations are confirmed, the action is approved and appended to the mathematically sealed Hash-Chained Write-Ahead Log (WAL).

---

## Getting Started

### Prerequisites
- **Go 1.24+**
- **Python 3.10+** (For simulation testing)
- OpenSSL (for generating local testing certs, included by default on most systems)

### 1. Installation

Clone the repository and build the core binaries:
```bash
git clone https://github.com/PandiaJason/bats.git
cd bats

# Install dependencies (if any)
go mod tidy
```

### 2. Bootstrapping a Cluster

Start your bootstrap node (Node 1):
```bash
# Node 1 acts as the initial genesis leader
go run cmd/node/main.go node1 8001
```

In separate terminal windows, spin up additional replica nodes. They will automatically sync to Node 1:
```bash
PEERS="localhost:8001" go run cmd/node/main.go node2 8002
PEERS="localhost:8001" go run cmd/node/main.go node3 8003
PEERS="localhost:8001" go run cmd/node/main.go node4 8004
```

### 3. Elastic Scaling (Adding Nodes at Runtime)

BATS v2.0 supports dynamic horizontal scaling. Use the built-in CLI join-tool to attach a new node to a live cluster:
```bash
# Deploys node 5 and forces a cluster-wide View Change to update quorum
go run cmd/join-tool/main.go localhost:8001 node5 8005
```

---

## Testing & Agent Simulation

We provide a full simulation suite out of the box to prove BATS's efficacy in blocking autonomous agent hallucinations and adversarial prompts. This uses our OpenClaw Python Wrapper to mock native framework requests directly to BATS.

### Run the Agent Gauntlet Simulation
```bash
chmod +x scripts/test_simulation.sh
./scripts/test_simulation.sh
```

### Real-World Cluster Benchmark (v3.2)

The benchmark boots a real 4-node PBFT cluster over mTLS HTTP/2, warms TLS connection pools across all endpoints, then fires HTTPS requests through the full safety pipeline. Measures end-to-end: AI heuristic → cryptographic signing → consensus broadcast → WAL persistence.

```bash
go test -v -timeout 60s ./tests/ -run TestBenchmarkLatency
```

| Action Type | p50 | p95 | p99 | Verdict |
| :--- | :--- | :--- | :--- | :--- |
| **SAFE_READ (Fast Bypass)** | **675µs** | **1.76ms** | **1.76ms** | Optimistic Approval |
| **SAFE Write (Sync PBFT)** | **6.5ms** | **7.8ms** | **7.8ms** | Full Quorum Commit |
| **UNSAFE (Immediate Reject)** | **368µs** | **525µs** | **525µs** | **BLOCKED** |

> **Performance:** Fast-path reads defer all I/O (WAL, logging, PBFT) to background goroutines, keeping p50→p95 variance under 3x. Benchmarks include TLS warmup for steady-state accuracy.

---

## Native Integrations

#### 1. OpenClaw (Python)
BATS completely intercepts Python-driven workflows. Wrap your agent outputs using the vetted connector client:
- **SDK Path**: `integrations/openclaw-wrapper/bats_vettor.py`
- Setup: Initializes a robust HTTPS connection with the cluster using the root CA bundle.

### 2. n8n Automation
BATS comes with a dedicated node template for n8n to act as a choke-point before crucial orchestration steps.
- **Node Path**: `integrations/n8n-node/`

---

## The Gauntlet CLI

The `bats` CLI includes a high-tier adversarial testing suite ("The Gauntlet") to verify cluster resilience against active Byzantine threats.

```bash
# Running the Xs10s Adversarial Gauntlet...
> bats-cli gauntlet --target=./swarm_config.json --f=1

[DETECTED] Node_4 attempted Payload Mutation (ASI03) -> [BLOCKED]
[DETECTED] Node_2 attempted Replay Attack (ASI07)    -> [BLOCKED]
[RESULT]   System Resilience Score: 100% (no successful adversarial commits)
```

---

## Threat Model & Security Guarantees

BATS adheres to strict mathematically provable models to protect against the OWASP Top 10 for LLM Applications (2026 specs).

| Threat Vector | Mitigation Strategy |
| :--- | :--- |
| **Agent Hallucinations** | Vetted through multi-model "Council of Agents" PBFT voting. |
| **Malicious Prompt Injections** | Hard-blocked by the pre-consensus Heuristic Safety Gate. |
| **Node Compromise (Byzantine)**| Tolerates up to $f$ actively malicious nodes dropping/forging packets. |
| **Network Eavesdropping** | AES-256 GCM encrypted mTLS tunnels via QUIC / HTTP/2. |

**Performance Metrics**
- **Latency**: 5-20ms intra-region consensus commits.
- **Throughput**: Sustains 1k-10k TPS depending on WAL IOPS.

---

## Configuration & Environment Variables

BATS nodes can be heavily customized through environmental flags:

| Variable | Description | Default |
| :--- | :--- | :--- |
| `PEERS` | Comma separated list of active nodes to sync with at boot. (`"NONE"` for standalone) | `localhost:8001,...` |
| `AI_PROVIDER` | LLM backend for complex heuristic validation (openai, anthropic, google). | `openai` |
| `OPENAI_API_KEY` | Overrides the local mock heuristics with real GPT-4o verification. | `""` |

---

## Contributing

We welcome contributions to making AI orchestration fundamentally safer.
1. Fork the Project
2. Create your Feature Branch (`git checkout -b feature/AmazingSecurity`)
3. Commit your Changes (`git commit -m 'Add some AmazingSecurity'`)
4. Ensure all tests pass (`go test ./...`)
5. Push to the Branch (`git push origin feature/AmazingSecurity`)
6. Open a Pull Request

## License

Distributed under the MIT License. See `LICENSE` for more information.

---
<div align="center">
  <i>BATS was originally developed by <b>Xs10s Research</b>.</i><br>
  <i>Empowering autonomous agents through zero-trust architectures.</i>
</div>
