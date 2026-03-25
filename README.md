# BATS: Byzantine Agent Trust System

**The most resilient way to build trusted agent networks.**

---

## 🌟 Simple Quick Start

Don't want to read the science? Just run this:

1. **Start the Cluster**: `go run ./cmd/bats/main.go start`
2. **Watch it Move**: Open [http://localhost:8080](http://localhost:8080)
3. **Trigger Consensus**: `go run ./cmd/bats/main.go trigger`

That's it. You now have a 4-node Byzantine-resilient cluster running with mTLS and Protobuf.

---

## 📖 What is BATS?

BATS is a system that allows 

BATS implements a modernized version of **Practical Byzantine Fault Tolerance (PBFT)**. It ensures safety (agreement) and liveness in an asynchronous network provided that no more than $f$ nodes are faulty in a cluster of $3f+1$ nodes.

### Consensus Protocol

The protocol operates in three distinct phases to achieve a "Quorum Certificate":

1.  **Pre-prepare**: The designated Leader broadcasts a `PREPREPARE` message with a cryptographically signed digest of the proposed state transition.
2.  **Prepare**: Nodes validate the proposal and broadcast a `PREPARE` message. A node enters the 'prepared' state once it collects $2f+1$ matching messages.
3.  **Commit**: Nodes broadcast a `COMMIT` message. The state transition is finalized and written to the **Write-Ahead Log (WAL)** only after $2f+1$ commit messages are verified.

### Safety & Liveness
- **Safety**: Ensured by the intersection of quorums ($2f+1$) which guarantees that no two conflicting transactions can be committed.
- **Liveness**: Guaranteed by a **View Change** mechanism that rotates the leader if a consensus round times out due to a faulty prime node.

---

## 🛠️ Developer Implementation

### Core Architecture

- **Transport Module**: Uses **mutual TLS (mTLS)** for peer-to-peer encryption and identity verification. Each node identifies itself using a certificate signed by the cluster's private Root CA.
- **Serialization Layer**: Built on **Protobuf (v3)**. Binary encoding reduces payload size by ~65% compared to JSON, significantly lowering network latency.
- **Persistence (WAL)**: A high-performance, append-only JSON logger that ensures atomicity of state commits.
- **Networking**: Asynchronous broadcasting with an exponential backoff retry policy for transient network partitions.

### Technical Stack
- **Language**: Go 1.22+
- **Security**: Ed25519 (Digital Signatures), SHA-256 (Hashing)
- **Deployment**: Docker Compose & Kubernetes-ready (Configurable via ENV)

---

## 🚀 Getting Started

### 1. Requirements
- Go 1.22+
- Docker & Docker Compose (optional)
- Protobuf Compiler (for protocol changes)

### 2. Launch the Cluster
The easiest way to start a 4-node cluster ($f=1$) with a monitoring dashboard:

```bash
# Using the Unified CLI
go run ./cmd/bats/main.go start
```

### 3. Monitoring (Real-time Dashboard)
Navigate to [http://localhost:8080](http://localhost:8080) to view:
- **Node Heartbeats**: Live health monitoring of peer connections.
- **Quorum Delta**: Real-time status of the $2f+1$ agreement threshold.
- **View Status**: Current leader and epoch information.

### 4. Direct CLI Controls
```bash
# Manually trigger a consensus round
go run ./cmd/bats/main.go trigger

# Inspect the WAL for a specific node
go run ./cmd/bats/main.go audit
```

---

## 🔐 Security & Trust

BATS is built on a **Zero-Trust** model. Even if a node's transport layer is breached, individual transaction integrity is maintained via:
- **Source Authentication**: Every message is signed by the node's private Ed25519 key.
- **mTLS Enforced**: Nodes will reject any handshake not originating from a known BATS-CA signed certificate.
- **Auditability**: The WAL provides an immutable, verifiable trail of all committed transitions.

---

## 🗺️ Roadmap: The Path to ₹1Cr
- [x] Protobuf & TLS Upgrades
- [x] Basic View-Change Implementation
- [ ] **Leader Election**: Dynamic weightage-based election.
- [ ] **QUIC Transport**: Move from HTTPS to QUIC for even lower latency.
- [ ] **State Pruning**: Automated WAL checkpointing and snapshotting.

---
*Created with ❤️ for high-trust agent networks.*
