# Privacy and network boundaries

Startup loads only local files and serves local assets. There are no remote fonts, favicons, analytics, scheduled scans, automatic hosted AI calls, or cloud fallback. Claude CLI requests require explicit selection and consent.

System timezone checks read the current operating-system configuration on each scan. A process-level `TZ` override is not treated as the system setting. If the current offset cannot be read, the comparison remains unknown rather than reusing a cached value.

| Action | Destination | Data observed or sent |
| --- | --- | --- |
| Local check | Fixed OS commands on this device | OS/architecture, timezone, locale, counts and proxy/DNS/route summaries |
| Network check | api.ipify.org, api6.ipify.org | Browser and agent egress addresses; providers see request metadata |
| Clock comparison | api.ipify.org, www.cloudflare.com | HTTPS request metadata and response Date headers |
| IP intelligence, optional | Selected ipapi.is or compatible endpoint | Observed public IPs and configured API key in the request body; public endpoints require HTTPS |
| WebRTC, optional | Configured STUN host | UDP routing and public candidate addresses |
| Service check, optional | Saved public HTTPS origin, port 443 | One HEAD request; no login, cookies, redirect following, or user path |
| Access research, optional | One to three selected public HTTPS pages, port 443 | GET requests without cookies/authentication; one homepage may add up to two relevant same-host links; each host sees request metadata |
| Access research summary, optional | Selected and confirmed assistant | Service name/origin, public excerpts, source URLs, retrieval times, and fetch failures; no saved issues, notes, IPs, or diagnostic reports |
| Career jobs, optional | Recognized Greenhouse, Ashby, Lever API routes or a selected public HTTPS page | User-requested GET without cookies/sign-in; provider sees board/posting identity and request metadata |
| Career updates, optional | Saved public HTTPS RSS/Atom/news sources | User-requested GET; no cookies, sign-in, redirects, scripts, or article crawling |
| OpenRouter usage, optional | openrouter.ai/api/v1/datasets/rankings-daily | Saved key in Authorization header and requested UTC date window; key never goes to job/news sources |
| Career advice, optional | Selected and confirmed assistant, or manual clipboard handoff | Exact previewed question/selected evidence and opted-in student profile; no key, usage rows, diagnostics, or prior advice |
| Model discovery | Configured loopback runtime | Fixed model-list/metadata requests; optional process-supplied auth token |
| Local-model question | Configured loopback runtime | Previewed context, reviewed opt-in notes, question, and same-context chat history |
| CLI detection | Installed executable on this device | Version/help invocation; no report or question |
| Claude CLI question | Installed CLI and its configured provider | Previewed context, opt-in notes, question, and same-context history; administrator policies/hooks may apply |
| Copy reviewed prompt | System clipboard | Selected context, fixed instructions, and current question; no automatic submission to Codex |

The runtime itself may log requests or forward them elsewhere. Verify its local-only settings before confirming locality. This application cannot audit an arbitrary compatible server.

Service template import and export are local operations. Templates contain only a user-authored name, kind, origin, and policy; local IDs, status, notes, issue records, reports, credentials, and settings are excluded. Names and policy URLs/text can still identify private information, so review the preview before sharing. Pasted JSON is validated by the loopback server without fetching URLs and opens an unsaved service draft. Only **Save service** creates a new record; no existing service is overwritten. Imported policy dates are preserved and do not establish independent verification.

Access research accepts public HTTPS pages without query parameters, fragments, or embedded credentials. Every direct connection validates and pins public DNS results. Configured HTTP/SOCKS proxy routes are declined for this feature when destination enforcement cannot be established; there is no direct fallback. Redirects, page scripts, sign-in flows, downloads, and embedded resources are not followed. Up to 2 MiB is read per page, with at most 6 KiB of extracted UTF-8 text per source; the aggregate serialized model context can shorten excerpts further. The displayed truncation flag identifies partial extracts. HTML extraction is a limited text reader, not a rendered-browser view. Source pages can be incomplete, undated, contradictory, or inaccessible. The model's cited interpretation needs human review and does not update saved policy rules or scores. Research results stay in request/browser memory and are not written to the store.

Raw network evidence is retained in application/browser memory for the current check. Saving a report retains an explicit summary projection: score, findings, source/policy metadata, timestamp, and coarse OS/timezone information, without raw IPs or provider payloads, including company/ASN organization names, types, network ranges/routes, and raw abuse proportions. The existing local evidence details show those optional fields as text only for the current in-memory check. Saved/redacted exports and model context use generic finding explanations; no service-private blocklist is collected or inferred. Free-text policy source URLs may identify a service; exports are not anonymous. Profile exports omit notes and error messages unless explicitly included. Review every export preview. Copy JSON writes that same reviewed snapshot to the system clipboard only when clicked; clipboard history and other applications may retain it.

Service selection/report changes reset the browser chat; server conversations are bound to a profile, report, settings, and a 30-minute lifetime. Conversations are not serialized. A canceled question may already have reached the runtime; cancellation cannot retract received data.

Career entry reads local state only. Company/profile/key saves and pasted evidence do not fetch remote sources; job/news/usage refreshes are explicit. Known ATS and usage calls use the configured agent route. Custom career pages/feeds use the same public-DNS enforcement and proxy refusal as access research, without direct fallback. Refresh errors preserve old evidence. Career evidence, student profile, usage snapshots, and saved advice persist in the private local store; unsaved model output stays in request/browser memory. Diagnostic `/api/state`, diagnostic reports, and diagnostic exports omit the Career section. Advice notes are saved only by explicit action and remain user-reviewed, unverified guidance. See [Career Radar](career-radar.md) for bounds and exact context selection.

The store, its prior-file backup, and the dedicated pre-migration version-1 backup may contain sensitive user-authored notes and saved credentials. IP/OpenRouter keys remain plaintext in the private store and any backups containing them. Keys are excluded from API responses, exports, and assistant context. Removing a saved key does not erase previous backups. Clipboard history and receiving applications can retain copied prompts. Do not enter credentials, cookies, recovery codes, or identity documents. Limits: 200 services, 50 issues per service, 200 saved reports, 16 MiB total store. Reaching a limit fails visibly without automatic deletion.
