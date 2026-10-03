# Source distribution verification

## Final security review — 2026-10-03

The final source review added these corrections and regressions:

- Claude Career analysis now accepts only the exact built-in Career or diagnostics system instruction at message index zero and passes the chosen instruction to the CLI. Custom, modified, duplicate and misplaced systems remain rejected. Actual synthetic CLI execution and protected Career preview/analyze tests verify exact argv, reviewed stdin context, isolation switches, private-data exclusion and no automatic note save. The focused CLI race suite passed; no real CLI account was contacted.
- Custom JSON-LD postings retain bounded explicit identifiers when no safe posting URL exists. Distinct URL-less postings with different identifiers now refresh without identity collisions; tests protect stable IDs across reordered refreshes, unsafe identifiers and conservative rejection of ambiguous records.
- Career analysis locks draft editing and saved-note copies until completion, so a delayed answer cannot overwrite edits made during the request. Local reload blocks mutations without discarding a typed key and resumes saves at the fresh revision. Usage shows the canonical OpenRouter/as-of/CC BY 4.0 attribution and license link.
- Fresh `node --test tests/ui.test.cjs tests/career.test.cjs` passed **79/79** tests, with none skipped. Earlier 76-test results below precede these additional regressions.

A `govulncheck` v1.8.0 audit of the installed Go 1.26.1 toolchain reported 15 reachable standard-library advisories. Earlier successful tests on that toolchain remain historical functional evidence, not security acceptance. The source now requires Go 1.26.8; builds on the supported Go 1.27 branch require 1.27.1 or later. Always select the latest security patch on a [supported Go branch](https://go.dev/doc/devel/release). The existing OS/Go CI matrix is retained, with one pinned vulnerability-scanner gate on Linux using the minimum supported Go branch.

Final local verification used Go 1.27.1 on macOS amd64: fresh `go test -race -count=1 ./...` passed both packages, `go vet ./...` passed, and a trimmed production build succeeded. Both JavaScript syntax checks and all 79 Node tests passed. `govulncheck` v1.8.0, rebuilt with that same compiler, reported **No vulnerabilities found**. Formatting and diff-whitespace checks passed. The included Go runtime and vendored license notices match the verified toolchain. These results do not establish the security of a binary built with an older compiler.

The final distribution uses the existing 67-file allowlist; its manifest and archive are regenerated from the reviewed sources before publication. Source content checks exclude machine paths, signing identifiers and common credential formats; local stores, Xcode state, Git history and development records are outside the allowlist. This is a scoped source review and automated scan, not a guarantee that no defects exist. Native platform CI is checked on the PR before merge; authenticated OpenRouter access and real Claude/local-model quality remain outside these fixture-based tests.

## Earlier Career Radar verification — 2026-10-03

Earlier integrated functional verification on macOS amd64 with Go 1.26.1 and Node.js 22.16.0 passed:

- `go test -race -count=1 ./...`: both packages passed (root 3.987s; internal/app 28.044s). `go vet ./...` and `go build ./...` passed.
- Both `app.js` and `career.js` syntax checks passed. `node --test tests/ui.test.cjs tests/career.test.cjs` passed 76/76 tests, with none skipped. CI explicitly includes both files and retains the Linux/macOS/Windows and supported-Go matrix.
- Linux amd64 and Windows amd64 builds with CGO disabled passed; Windows app tests compiled successfully. Temporary build outputs were kept outside the source distribution. Cross-compilation does not establish native runtime behavior.
- Compiled-app browser acceptance used a disposable store and local mock model: company create/edit/delete with evidence cascade, literal pasted evidence, graduate filtering, failed refresh preserving old evidence, exact selected context with profile opt-in off, mock analysis with no automatic note save, explicit note save/reload/delete, and Codex copying messages only. Existing Services/Diagnostics loaded; native dialogs and 390-pixel Jobs/Advice views were checked without page overflow; the final fresh page had no console errors. Mock API transport verified the messages sent match the preview. No real user store or paid account was used.
- Public-source checks returned Mistral 209 jobs as a complete board in the actual browser; the Anthropic adapter retained 500 jobs as an incomplete capped board. OpenAI's official RSS produced 20 items with source publication dates. These are point-in-time checks, not continuing availability guarantees.
- Original-byte version-1 recovery and migration conflict/no-overwrite behavior have regression coverage. The ATS client timeout regression proved the old eight-second overall budget failed the boundary test; fixed known ATS reads allow 30 seconds while retaining header/TLS/proxy boundaries and custom-source timeout/DNS enforcement. Cancellation, timeout, unreadable, oversized, and invalid UTF-8 bodies have distinct sanitized errors.

The reviewed handoff reconciled 27 explicit development paths after all 54 original development hashes matched. The source directory and ZIP contain exactly 67 allowlisted files. All file contents match the reviewed publication checkout, and the 66 SHA-256 entries exclude the manifest itself. Archive entries use relative paths and a fixed ZIP epoch with no extra/comment fields; private stores, credentials, native signing state, Git history, and planning files are excluded. Content scans found no developer-specific paths, signing identifiers, or common credential formats. This is a targeted integrity/content check, not an independent security audit. Authenticated OpenRouter access, real Claude/local-model quality, and native Windows/Linux runtime checks remain unverified. The earlier checks below describe the pre-Career source snapshot; their 51-test count is historical.


## Earlier diagnostics verification — 2026-10-03

Verified on 2026-10-03 with Go 1.26.1 and Node.js 22.16.0 on macOS (amd64).

## Completed checks

- Fresh Go tests with the race detector: `go test -race -count=1 ./...` passed for both packages.
- Static analysis: `go vet ./...` passed.
- Browser JavaScript syntax: `node --check internal/app/web/app.js` passed.
- UI regression tests: `node --test tests/ui.test.cjs` passed all 51 tests, with no skipped tests.
- Linux amd64 and Windows amd64 production builds passed with CGO disabled. Windows amd64 app tests also cross-compiled successfully. Build outputs were kept outside this source directory; compilation does not execute those tests.
- Isolated browser acceptance used synthetic observations and a disposable store. It verified v1.1 raw company/ASN scopes, known zero versus unknown ratios, and literal rendering of script-looking organization text. An older saved v1.0 report retained its original hosting weight; comparison across scoring versions was refused. Save, export preview, and assistant context preview retained the network-reputation finding while excluding raw IPs, organization names, networks/routes, and ratios. No real provider or model calls were made.
- Go formatting and diff whitespace checks passed. Focused JSDOM checks covered native form validity, dialog naming, and editor closing.

## Review corrections

- v1.1 separates IP-level abuse, explicit hosting, and network reputation while retaining the 100-point total. Optional malformed metadata remains unknown without discarding valid country or provider flags. Company networks and ASN routes must contain the observed IP and match its family. Numeric band boundaries, worst-exit combination, missing sources, metadata redaction, and AIvia request branding have regression coverage. Existing v1.0 snapshots keep their stored scores and weights.
- Windows compatibility tests preserve store/revision checks while applying the POSIX private-mode assertion only where those permissions are exposed. The proxy case-variant test expects Windows' final environment value to win without case sensitivity; malformed active proxy configuration is still rejected. Native Windows rerun is pending.
- Malformed active proxy environment settings stop non-loopback requests instead of silently falling back to direct connections. Local loopback and valid `NO_PROXY` rules remain supported; error messages hide credentials.
- System timezone observations read current OS configuration on each scan; unavailable data stays unknown.
- Public connections reserve timeout budget for remaining validated DNS addresses when an earlier address stalls. Cancellation still stops subsequent attempts.
- The assistant rejects explicitly reported truncation, tool actions, and abnormal completion reasons without retrying. Missing completion metadata remains compatible and cannot reveal silent truncation.
- Service and issue editors prevent duplicate saves, stale completion, and reopening with an outdated revision while a previous save refreshes. Hidden inactive policy controls no longer block native form validation. Stale exports cannot open an empty dialog; all four reviewed dialogs have accessible names.
- Full network-collector fixture tests cover IPv4/IPv6 results, canonical IP deduplication, unavailable IPv6, and provider failures. Network boundaries use local fixtures; no production TLS setting was weakened.

Independent review found no remaining actionable findings within these corrections. This is a scoped engineering review, not a guarantee that the entire application has no defects.

## Distribution contents

Runtime code, tests, and the example template are identical to the reviewed working sources. Only public documentation, CI, and ignore rules were adapted for this source distribution. The web assets are embedded by Go; no Xcode project or native launcher is required.

The directory was assembled from an explicit file allowlist. Git history, local application stores and backups, environment files, account configuration, developer tooling state, Xcode projects, Apple signing material, screenshots, native artwork, and build products were excluded. Source files and documentation were checked for developer-specific paths, signing identifiers, and common credential formats; no matches were found. This is a targeted distribution check, not an independent security audit.

`SHA256SUMS` lists every supplied file except itself. It checks content integrity, not publisher identity. The accompanying ZIP stores file content and relative names only, without macOS extended attributes, resource forks, user/group names, or original file timestamps. macOS may attach its own system provenance attribute to files on disk; it is not included in the ZIP or file-content checksums.

## Verification limits

Cross-compilation does not establish behavior on real Windows/Linux devices or other CPU architectures. CI for the initial source snapshot passed on Linux/macOS and failed on Windows because of the two platform assumptions described above. The corrected v1.1 source has not had a native Linux/Windows rerun. Automated network and model checks use local fixtures; they do not verify current provider policy, real account eligibility, paid API access, or every user's installed model/CLI runtime.

Scores are heuristic evidence ranges, not calibrated probabilities of account acceptance. Missing or stale evidence stays unknown. GitHub private vulnerability reporting is enabled; use the [private reporting form](https://github.com/herobuildsAI/AIvia/security/advisories/new) described in [SECURITY.md](../SECURITY.md).
