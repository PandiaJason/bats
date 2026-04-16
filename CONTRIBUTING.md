# Contributing to WAND (Watch. Audit. Never Delegate.) is a high-integrity, **deterministic safety** coordination layer. We maintain strict engineering standards to ensure that the "wire" beneath autonomous agents remains secure and deterministic.

## 🎖️ Engineering Philosophy

We follow a **deterministic safety approach. A single bug in the policy engine can compromise the safety of the entire agent swarm. Therefore, we do not "vibe-code." Every change must be backed by:
integrity of the protocol, all contributions must adhere to the following rigorous standards.

---

## 🛡️ Engineering Standards

### 1. Deterministic Execution
All policy-engine logic must be strictly deterministic. 
- **Prohibited**: Raw map iterations, unseeded random number generation, or state transitions dependent on local system time.
- **Requirement**: No Pull Request will be merged without passing the **Xs10s Adversarial Gauntlet**, which tests for drift under simulated network partitions and message reordering.

### 2. Memory Safety & Buffer Integrity
To mitigate payload mutation vulnerabilities, WAND enforces strict memory hygiene.
- **Fixed-Size Buffers**: Use fixed-size binary buffers (e.g., `[64]byte`) for all cryptographic handles.
- **Pointer Discipline**: Minimize pointer arithmetic and ensure all shared state is protected by the WAND thread-safety primitives.
- **Serialization**: Only Protocol Buffers (v3) are permitted for over-the-wire data to ensure deterministic binary representation.

### 3. Formal Verification
Any logic affecting the **policy engine or WAL must be accompanied by a **Proof of Safety**.
- Contributors must document the safety invariant that their change preserves$).
- Changes to the 3-phase commit protocol require a formal trace analysis showing that safety holds even during a View Change.

---

## 🛠️ Development Workflow

1.  **Issue Scoping**: Significant architectural changes must be discussed in a formal RFC issue before implementation.
2.  **Linting & Analysis**: Code must pass `golangci-lint` with the strict project profile, including static analysis for race conditions (`go test -race`).
3.  **Adversarial Testing**: Integration tests must demonstrate resilience against the **Gauntlet CLI** scenarios (Replay attacks, message forgery, and node dropout).

## ⚖️ License & Credits

By contributing to WAND, you agree that your contributions will be licensed under the project's **MIT License**.

---

*Authored by Xs10s | Xs10s Research (2026)*  
*Maintaining the wire beneath the intelligence.*
