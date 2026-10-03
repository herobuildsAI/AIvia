# AIvia

A local, English-language workspace for diagnosing website and app access problems and researching early-career CS opportunities. Manage services such as Muse, keep an issue journal, inspect your browser/system/network environment, and chat with a local model or Claude Code, with optional redacted reports. Codex is supported through a reviewed-prompt clipboard handoff.

**One executable. A browser interface. No hosted backend, telemetry, npm install, or third-party Go modules.**

## Run

Use Go 1.26.8 or newer on a [supported release branch](https://go.dev/doc/devel/release); Go 1.27 requires 1.27.1 or later. Always use the latest security patch of your chosen supported branch. Rebuild older binaries with a patched toolchain.

```sh
go run .
```

Or build an executable:

```sh
go build -trimpath -o aivia .
./aivia
```

This is a source-only distribution. Open a terminal in this directory and build or run it on your own device. macOS, Linux, and Windows use the same Go application and browser interface; no Xcode project or Apple signing configuration is required.

On Windows, build with `go build -trimpath -o aivia.exe .` and run `./aivia.exe` in PowerShell. The application opens a browser at a randomly selected `127.0.0.1` port. Keep the terminal running; Ctrl+C stops it. Startup errors return a nonzero exit code and release the data-directory lock.

```sh
./aivia -no-browser -port 8765 -data-dir ./data
./aivia -proxy http://127.0.0.1:7890
```

`-proxy` affects agent diagnostic requests. Browser requests use the browser's own route. OS/PAC proxy settings are observed but not automatically applied. `HTTPS_PROXY`, `HTTP_PROXY`, and `NO_PROXY` are honored by the Go transport when no explicit proxy is supplied. `ALL_PROXY` is not automatically honored. Prefer environment configuration for authenticated proxies to avoid command-line credential exposure.

Malformed proxy environment values stop non-loopback agent requests instead of silently allowing direct connections. Fix or unset the relevant value before retrying. Valid `NO_PROXY` rules and local loopback bypass remain supported.

## Workflow

1. **My services:** add a Website or App, optionally provide its public HTTPS origin, and record registration/login/access issues. Muse is a user-entered example; no domain or policy is guessed.
   **Service templates:** share a definition without private notes, issues, reports, or local IDs. Use **Import template** to paste a shared JSON definition, review it, and save it as a new service. Imported policies keep their source/date and remain user-provided. See the [template format and contribution guide](CONTRIBUTING.md#sharing-service-templates).
   **Research access rules:** review suggested official sources and choose **Fetch & summarize**. Claude domains have country/help/status suggestions. Other services start with their saved policy URL or homepage; you can supply up to three official URLs. A single homepage can contribute up to two relevant links on the same host. The selected assistant summarizes public extracts with numbered sources and unknowns; **Fetch sources only** works without a model. Names alone do not identify a product, and this is bounded page reading, not a general web-search engine.
2. **Diagnostics:** start with local checks. Network checks explicitly contact ipify and Cloudflare. Optional controls enable IP intelligence, STUN, or an origin-only HTTPS HEAD request.
3. **Review:** inspect the 0–100 risk range and its explanation. Findings are grouped into risk signals, unknowns, and checks without an adverse signal. Up to three next checks prioritize observed signals and the largest remaining evidence gaps; related checks share one step. Shortcuts open source research or IP settings without running a probe or sending a question. Save or export a redacted snapshot explicitly. Compare it with an earlier snapshot of the same service; coverage and rule changes remain visible. Use Copy JSON if your browser cannot download files.
4. **Assistant:** configure Ollama, a compatible local server, or an installed Claude Code CLI. Confirm the appropriate local/CLI consent, then chat directly without creating a service or report. Optionally enter the failure stage and reviewed error text. To attach a diagnostic report, switch to report mode and preview it before sending. For Codex, copy the reviewed prompt into your own client; automatic Codex execution is unavailable.

5. **Career Radar:** save companies, explicitly refresh supported recruiting/news sources or paste evidence, filter opportunities, and review selected context before asking your configured assistant. Student-profile inclusion is opt-in; advice is saved only when you choose. Open original postings to apply manually. See [setup, source formats, freshness, and recovery](docs/career-radar.md).

The assistant provides analysis and manual guidance. The Claude connector disables model tools, but an installed CLI's administrator policies and hooks still apply. The app does not register accounts, switch VPNs, install models, or perform CLI sign-in.

## What the index means

The index summarizes observed signals; **it is not a probability of being blocked**. Missing or failed checks widen the interval. An offline check with matching browser/system offsets typically yields **0–97 with 3% coverage**, not a claim that the IP is clean. The weights are transparent engineering heuristics, not trained or calibrated against account outcomes.

IP intelligence is optional. Configure your own ipapi.is key or a self-hosted compatible endpoint in **Settings → IP intelligence**. The built-in provider also accepts `AIVPN_IPAPI_KEY` from the process environment. Saved keys remain plaintext in the local store and backup, never in API responses, exports, or model context. Enable the lookup explicitly for each selected configuration. Without provider data, location and reputation remain unknown. See [configuration and compatible protocol](docs/ip-intelligence.md). No third-party IP database is bundled.

Claude web/API starter policies include a source and review date. Policies older than 30 days become unknown. Check official policy before applying it; geographic support does not establish account eligibility.

## Data and recovery

AIvia is the display name. The existing `aivpn-tools` data directory and `AIVPN_*` environment variables are retained for compatibility.

The startup message prints the data directory. Defaults follow Go's `os.UserConfigDir`: macOS Application Support, Windows AppData, and Linux XDG configuration directories.

- `store.json`: version-2 profiles, notes, settings, saved IP/OpenRouter API keys, redacted reports, and Career Radar companies, evidence, optional student profile, usage snapshot, and explicitly saved advice.
- `store.json.v1.bak`: exact pre-migration version-1 bytes, preserved separately from the rotating backup. An old binary cannot read version 2; see [safe rollback](docs/career-radar.md#version-2-and-recovery).
- `store.json.bak`: the previous store, retained for recovery. Deleted records can remain here until another write.
- `store.lock`: exclusive ownership marker. A second process using the same directory refuses to start.

Records are plaintext. Unix directories/files are restricted to the current user; Windows access relies on the user's directory ACLs. Raw IP observations and chat history are not written to the store. Other processes running as your user can still read local data.

If startup reports a lock, first verify that no AIvia process uses that directory. After a crash, archive the directory and remove only the stale `store.lock`. A corrupt `store.json` is never silently overwritten. Stop the app, keep a copy, validate the backup JSON, and restore `store.json.bak` to `store.json` if appropriate. A leftover `store.json.tmp` is not automatically recovered; preserve it before removing it to retry. There is no secure-erasure promise for backups or filesystem snapshots.

## Scope and limits

- Cross-platform collectors use read-only OS metadata; unavailable commands produce unknowns. Cross-compilation is not real-device validation.
- Browser egress, agent egress, and optional public WebRTC candidates are separate observations. Split routes and rotating exits can differ legitimately.
- DNS settings are summarized; there is no controlled DNS leak test. No invasive fingerprint or browser extension inventory is collected.
- A target probe tests only HTTPS response reachability, never account registration. Redirects are not followed; proxy-routed target probes are declined because destination enforcement cannot be guaranteed.
- The local-model mode trusts your verification of runtime locality. It rejects known cloud models, but cannot prove a custom local server never forwards requests.
- Claude CLI execution is implemented on macOS/Linux with compatibility checks; Windows can use the copied-prompt workflow. Authentication, model access, and quota depend on each user's CLI account; fixture tests do not verify that account. CLI hooks configured by an administrator remain part of the trusted runtime.
- The browser requires current support for Fetch, AbortSignal.any/timeout, structuredClone, and native dialog elements. A normal browser may provide better download support than an embedded web view.

## Development and verification

```sh
go test -race ./...
go vet ./...
node --check internal/app/web/app.js
node --check internal/app/web/career.js
node --test tests/ui.test.cjs tests/career.test.cjs
go install golang.org/x/vuln/cmd/govulncheck@v1.8.0
govulncheck ./...
```

Node.js 22 or newer is needed only for the UI tests; no npm installation is required. The Go race detector requires a supported C compiler. The included CI workflow checks Go, browser JavaScript, and builds on macOS, Linux, and Windows, and scans reachable vulnerabilities once on Linux with the pinned scanner. See the verification record below for checks actually run.

This directory includes source, tests, public documentation, and license notices. It excludes local data, Xcode projects, signing material, and build products. Preserve the license notices when redistributing source or binaries. `SHA256SUMS` verifies file integrity; it is not a publisher signature.

See [Career Radar](docs/career-radar.md), [scoring](docs/scoring.md), [privacy](docs/privacy.md), [assistant setup](docs/local-models.md), [IP intelligence](docs/ip-intelligence.md), [verification](docs/verification.md), [contributing](CONTRIBUTING.md), and [security](SECURITY.md).

Original source is [MIT licensed](LICENSE). Go runtime and third-party service/data/model terms are covered separately in [third-party notices](THIRD_PARTY_NOTICES.md). No affiliation with Anthropic, Muse, Ollama, or the network data providers is implied.
