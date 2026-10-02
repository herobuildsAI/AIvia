# Source distribution verification

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

Scores are heuristic evidence ranges, not calibrated probabilities of account acceptance. Missing or stale evidence stays unknown. Before opening a public repository, configure the private reporting channel described in `SECURITY.md`.
