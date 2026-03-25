# Contributing to BATS

Thank you for your interest in contributing to the Byzantine Agent Trust System (BATS). This project maintains rigorous engineering standards to ensure the integrity and safety of decentralized agent networks.

## 🛡️ Byzantine-Safe Coding Standards

All contributions must adhere to the following principles:

1.  **Deterministic Logic**: Ensure all consensus-related logic is perfectly deterministic. Avoid using unseeded random numbers, non-deterministic iteration orders (e.g., raw map iterations in Go), or time-dependent logic that affects state.
2.  **Zero-Trust by Default**: All new network interfaces must enforce mTLS and use Protobuf for serialization. No unauthenticated endpoints are permitted.
3.  **Memory Safety**: Pay close attention to buffer management and potential race conditions. All state interactions must be thread-safe.
4.  **Error Handling**: Failure of a single component must not compromise the entire node. Use robust circuit-breaking and graceful degradation.

## 🛠️ Development Workflow

1.  **Issue First**: Please open an issue to discuss significant changes before submitting a PR.
2.  **Linting**: Ensure all code passes `golangci-lint` with the project's specific configuration.
3.  **Testing**: All new features must include unit tests and, where applicable, integration tests within "The Gauntlet" environment.
4.  **Documentation**: Update the technical specification and README if your change affects the protocol or architecture.

## ⚔️ The Gauntlet Verification

Every Pull Request is subjected to "The Gauntlet"—our adversarial testing suite. Your code must demonstrate resilience against:
- Payload mutation attempts.
- Message replay attacks.
- Simulated Byzantine node behavior.

## 📜 Code of Conduct

Maintain a professional, technical, and respectful environment. We are building the infrastructure for the future of agentic intelligence.

---

*Authored by Xs10s | Xs10s Research (2026)*
