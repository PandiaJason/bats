# 🦇 BATS: Byzantine Agent Trust System

> **The zero-trust consensus backbone for the next generation of autonomous agent networks.**

[![Go Version](https://img.shields.io/badge/Go-1.22+-00ADD8?style=flat&logo=go)](https://golang.org/)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)
[![Security](https://img.shields.io/badge/Security-mTLS%20%7C%20Ed25519-blueviolet)](docs/SECURITY.md)
[![PRs Welcome](https://img.shields.io/badge/PRs-welcome-brightgreen.svg)](http://makeapullrequest.com)

BATS is a high-performance, **Byzantine Fault Tolerant (BFT)** consensus engine. It enables a cluster of distributed agents to reach an immutable agreement on state transitions, even when some nodes are compromised, offline, or malicious.

---

## 🚀 Quick Start (30 Seconds)

Experience the power of BATS cluster with zero configuration:

```bash
# 1. Launch a 4-node cluster immediately
go run ./cmd/bats/main.go start

# 2. Monitor the live health and quorum
# Open http://localhost:8080

# 3. Trigger a verifiable consensus round
go run ./cmd/bats/main.go trigger
```

---

## ✨ Features

- 💎 **PBFT Consensus**: Proven Practical Byzantine Fault Tolerance implementation.
- 🔐 **mTLS Security**: End-to-end encrypted node communication using mutual TLS.
- ⚡ **Protobuf Powered**: Binary serialization for ultra-low latency & high throughput.
- 🧠 **View-Aware**: Built-in leader election and view-change protocols for high availability.
- 📊 **Live Dashboard**: Real-time web-based telemetry and quorum monitoring.
- 💾 **Durable WAL**: Write-Ahead Logging for absolute data persistence.

---

## 🏛️ Architecture

BATS operates on a **Leader-Follower** model using a 3-phase commit protocol:

1.  **PROPOSE**: The leader broadcasts a new transaction hash.
2.  **VOTE**: Nodes verify the proposal and vote for its validity.
3.  **COMMIT**: Once a **$2f+1$ Quorum** is reached, the transaction is finalized across the cluster.

### Technology Stack
- **Engine**: Pure Go 1.22
- **Identity**: Ed25519 Digital Signatures
- **Transport**: HTTPS/2 with mTLS
- **Storage**: Append-only JSON WAL

---

## 🔬 For Researchers & Scientists

BATS is designed for formal verification and reliability research. The implementation adheres strictly to the safety and liveness properties defined in the original PBFT literature while introducing modern optimizations like:
- **Batched signing** for higher TPS.
- **Dynamic View-Change** triggers based on adaptive timeouts.
- **Minimal memory footprint** suitable for edge agent deployments.

---

## 🗺️ Roadmap

- [x] **mTLS & Protobuf Core** (V1.1)
- [x] **Interactive Dashboard** (V1.2)
- [x] **Basic View-Change** (V1.3)
- [ ] **QUIC Transport Support** (Upcoming)
- [ ] **K8S Operator for Auto-Scaling** (Upcoming)

---

## 📜 License

Distributed under the **MIT License**. See `LICENSE` for more information.

---
*Built for the future of verifiable agentic intelligence.*
