# BATS: Byzantine Agent Trust System

> **The zero-trust consensus backbone for the next generation of autonomous agent networks.**

[![Go Version](https://img.shields.io/badge/Go-1.22+-00ADD8?style=flat&logo=go)](https://golang.org/)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)
[![Security](https://img.shields.io/badge/Security-mTLS%20%7C%20Ed25519-blueviolet)](docs/SECURITY.md)

BATS is a high-performance, Byzantine Fault Tolerant (BFT) consensus engine. It enables a cluster of distributed agents to reach an immutable agreement on state transitions, even when some nodes are compromised, offline, or malicious.

---

## Simple Quick Start

1. **Start the Cluster**: `go run ./cmd/bats/main.go start`
2. **Watch it Move**: Open [http://localhost:8080](http://localhost:8080)
3. **Run AI-Verified Consensus**: `go run ./cmd/bats/main.go ai`

---

## Features

- **PBFT Consensus**: Proven Practical Byzantine Fault Tolerance implementation.
- **AI-Verified Decisions**: Ensure multiple internet-based agents (OpenAI/Anthropic) agree before committing.
- **mTLS Security**: End-to-end encrypted node communication using mutual TLS.
- **Protobuf Powered**: Binary serialization for ultra-low latency & high throughput.
- **View-Aware**: Built-in leader election and view-change protocols.
- **Live Dashboard**: Real-time web-based telemetry and quorum monitoring.

---

## ⚡ Unified CLI

The `bats` CLI is the main entry point for managing your cluster.

```bash
# Start cluster
go run ./cmd/bats/main.go start

# Run AI Agent consensus test
go run ./cmd/bats/main.go ai

# Trigger manual consensus
go run ./cmd/bats/main.go trigger

# View Audit Log
go run ./cmd/bats/main.go audit
```

---

## 📊 Web Dashboard

Once the cluster is running, access the real-time monitoring dashboard at:  
**http://localhost:8080**

- **Live Heartbeat**: See which nodes are online.
- **Quorum Status**: Real-time calculation of 2f+1 threshold.
- **Node Grid**: Health status of all cluster members.

---

## Architecture

BATS operates on a Leader-Follower model using a 3-phase commit protocol:

1.  **PROPOSE**: The leader broadcasts a new transaction or AI output.
2.  **VOTE**: Nodes verify the proof and vote for its validity.
3.  **COMMIT**: Once a 2f+1 Quorum is reached, the transaction is finalized.

### Technology Stack
- **Engine**: Pure Go 1.22
- **IA**: Integration with OpenAI & Anthropic
- **Security**: Ed25519 & mTLS
- **Serialization**: Protobuf v3

---

## Roadmap

- [x] mTLS & Protobuf Core
- [x] Interactive Dashboard
- [x] Internet AI Integration
- [x] Dynamic Weighted Leader Election
- [x] QUIC Transport Support (HTTP/3)
- [x] WAL State Pruning & Checkpointing

---

## License

Distributed under the MIT License. See `LICENSE` for more information.

---
*Built for the future of verifiable agentic intelligence.*
