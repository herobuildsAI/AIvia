# Source distribution verification

Verified on 2026-10-02 with Go 1.26.1 and Node.js 22.16.0 on macOS (amd64).

## Completed checks

- Fresh Go tests with the race detector: `go test -race -count=1 ./...` passed for both packages.
- Static analysis: `go vet ./...` passed.
- Browser JavaScript syntax: `node --check internal/app/web/app.js` passed.
- UI regression tests: `node --test tests/ui.test.cjs` passed all 48 tests.
- Source compilation passed for macOS, Linux, and Windows, each on amd64 and arm64, with CGO disabled. Build outputs were kept outside this source directory.
- Browser acceptance on the current build used a disposable data directory: saved a service after switching away from an invalid custom policy URL, added two issue records consecutively, and reloaded with both records preserved. No browser console warnings or errors were observed. The preview shut down cleanly and released its temporary store lock. No existing user data was used.
- Go formatting and diff whitespace checks passed. Focused JSDOM checks covered native form validity, dialog naming, and editor closing.

## Review corrections

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

Cross-compilation does not establish behavior on real Windows/Linux devices or other CPU architectures. The included GitHub Actions workflow is ready for a repository but was not run on a remote host for this snapshot. Automated network and model checks use local fixtures; they do not verify current provider policy, real account eligibility, paid API access, or every user's installed model/CLI runtime.

Scores are heuristic evidence ranges, not calibrated probabilities of account acceptance. Missing or stale evidence stays unknown. Before opening a public repository, configure the private reporting channel described in `SECURITY.md`.
