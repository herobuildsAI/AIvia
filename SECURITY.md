# Security

The application is a single-user local diagnostic tool. It binds only to loopback, checks Host/Origin and a per-process session token, restricts local HTTP model destinations to loopback, refuses redirects, and validates/pins public target destinations. Do not expose it through a tunnel, reverse proxy, shared host, or container port mapping.

The boundary protects against ordinary cross-origin websites, not malicious same-user processes, compromised browsers/extensions, or a dishonest local model runtime. Local HTTP is unencrypted; raw evidence and chat exist in process/browser memory. Notes, saved reports, and UI-saved IP keys are plaintext on disk and may remain in the previous-store backup. The optional Claude CLI adapter is a separately consented trust boundary: model tools are disabled, but administrator hooks and policies may still apply. Codex receives no context automatically; its integration is manual clipboard handoff. Review exports before sharing.

For security reports, use the repository hosting platform's private vulnerability reporting when enabled. If no private channel is configured, open a minimal issue asking for a private reporting channel without exploit details, credentials, or personal data. Before a public release, maintainers must enable private reporting or document a monitored security contact. Do not post sensitive reports publicly.

Dependency and toolchain updates should use supported security patch releases. This distribution contains source only. Distributors building binaries are responsible for applicable license notices, signing, and release provenance.
