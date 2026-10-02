# Contributing

Keep changes small, readable, and English-language. User-entered content may use any language. Contributions are accepted under this repository's MIT license; preserve author and third-party notices. Do not copy proprietary datasets, provider SDK code, branding, or model weights without checking their licenses.

Run the commands in README before submitting. Add behavioral regression tests for scoring, privacy boundaries, persistence, or network changes. Use loopback fixtures for automated tests, not live paid APIs. Document sources and dates for policy changes; do not infer restrictions from forum reports.

Privacy requirements: no telemetry or external startup requests, no credential logging, explicit opt-in for network probes, unknowns for failed checks, and only selected redacted evidence in model context. Never add assistant tools or automatic system changes without a reviewed design.

Use a separate temporary data directory for UI testing. Include OS/toolchain versions and precise validation limits in a pull request. Do not include real IPs, account details, tokens, or local store files.

## Sharing service templates

Use **My services → Share service template** to preview a reusable definition, then copy or download its JSON. Recipients choose **Import template**, paste the JSON, review it in the service editor, and save a new service. Import never overwrites an existing service or runs external checks. This is a reusable definition, not a backup/restore format; ordinary profile/report exports cannot be imported as templates.

The version-1 format uses `format: "aivia-service-template"`, `version: 1`, `name`, `kind` (`website` or `app`), `origin`, and `policy`. Start from [the example template](examples/service-template.json). It deliberately declares an unknown policy rather than guessing availability.

- Use official sources for the exact product and distinguish web, API, and subscription eligibility. A source URL is not proof that the template was independently verified.
- A `custom` policy requires uppercase ISO two-letter `countries`, an HTTPS `source`, and `checkedAt` (`YYYY-MM-DD`). Preserve the actual review date. Old and future dates remain unknown in scoring; importing does not refresh them.
- Use `worldwide` only with explicit supporting evidence; omit its country list. Use `unknown` without rule fields when evidence is insufficient. Templates cannot claim `builtin` identity or `provenance`.
- Optional `excludedRegions` maps country codes to region-name arrays. Names cannot contain commas or line breaks because the editor uses those separators. Country/subregion uncertainty remains subject to the normal scoring rules.
- The maximum JSON size is 256 KiB. Origins and policy URLs must use public HTTPS on port 443 with no credentials, query strings, or fragments; origins cannot have paths. Validation does not fetch or resolve those URLs.
- Share only factual rule data with links and review dates. Do not copy policy prose, logos, commercial datasets, credentials, or private account information. Check user-authored names and URLs before sharing, even though local IDs, status, notes, issues, reports, and settings are excluded.

Built-in starter rules export as custom dated snapshots. Imported rules remain user-provided and are never promoted to official or automatically updated. Policy contributions should include the source, what changed, date checked, and a sample showing expected supported/unsupported/unknown behavior. Automatic remote template subscriptions are not implemented.
