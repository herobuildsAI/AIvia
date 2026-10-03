# Career Radar

Career Radar is a local workspace for CS internship, new-graduate, and entry-level research. It stores companies, selected job/news evidence, an optional student profile, and explicitly saved advice notes. It does not rank employers, verify hiring eligibility, or submit applications.

## Start with your sources

Open **Career Radar → Companies → Add company**. Enter a name and, optionally, a website, recruiting URL, up to three news URLs, and model prefixes. Save the draft, then explicitly choose **Refresh jobs** or **Refresh updates**. Saving, entering the workspace, and opening an example draft do not fetch sources. Example companies are editable suggestions; only OpenAI's example includes a news RSS URL. Verify every source yourself.

Recruiting adapters recognize these hosted HTTPS shapes:

| Provider | Board | Single posting |
| --- | --- | --- |
| Greenhouse | `https://boards.greenhouse.io/BOARD` or `https://job-boards.greenhouse.io/BOARD` | Same host with `/BOARD/jobs/NUMERIC_ID` |
| Ashby | `https://jobs.ashbyhq.com/BOARD` | `/BOARD/POSTING_ID`, optionally ending `/application` |
| Lever | `https://jobs.lever.co/BOARD` | `/BOARD/POSTING_ID`, optionally ending `/apply` |
| Lever EU | `https://jobs.eu.lever.co/BOARD` | `/BOARD/POSTING_ID`, optionally ending `/apply` |

Supported hosted attribution parameters (`utm_source`, `utm_medium`, `utm_campaign`, `utm_term`, `utm_content`, and Greenhouse `gh_src`) are stripped before fetching. Other query parameters, encoded path segments, and query-based company wrappers are rejected. Fixed provider API routes are reconstructed from the board/posting identity.

Other recruiting and news sources must be public HTTPS URLs on port 443, without queries, fragments, or credentials. Custom recruiting pages can expose JSON-LD `JobPosting` records or a bounded text snapshot. News supports RSS/Atom, conservative dated HTML article extraction, and undated page snapshots. Company association is user-supplied; a fetched page does not establish authorship. There is no rendered browser, JavaScript execution, redirect following, sign-in, general search, or article crawling. If a page requires scripts, authentication, or browser verification, use **Paste job** or **Paste update**, include the source link when available, and review the literal text before saving.

Known ATS reads honor the configured agent proxy. Custom sources validate and pin public DNS destinations; proxy routes that cannot enforce that boundary are declined without direct fallback. Browser-opened application links follow your browser's own network route. See [privacy](privacy.md) for request destinations.

## Read evidence and freshness

Use **Jobs** to filter title, location, level, and workplace; job/update cards show at most 50 records per page. Level can be title-inferred, posting-explicit, or unknown. Missing location, employment type, publication dates, or remote status stay unknown. Published, last-published, updated, and retrieved dates have different meanings; source date-only values remain literal.

Refreshes are manual and source-specific. Failed refreshes preserve earlier evidence and show the attempt/error alongside the last success. Retrieval older than seven days, or absent retrieval, is labeled stale/freshness unknown; seven days is a display rule, not a guarantee of validity. A complete refresh of the same board may label a missing job **No longer listed**; this is not proof it closed. Single postings, capped/incomplete boards, and page snapshots cannot establish disappearance. News results are bounded samples, even after a successful fetch.

Fixed ATS fetches allow a 30-second overall read budget while retaining the existing eight-second response-header boundary. Custom/news sources keep the existing eight-second client timeout; their fetch context is bounded to 30 seconds. Fixed ATS responses are bounded to 16 MiB and at most 500 jobs; Lever boards use up to five 100-item pages. Custom/news responses are bounded to 2 MiB. Feeds scan at most 5,000 entries and return at most 20 deduplicated items; extracts are at most 6 KiB. These limits can omit opportunities. Open the original posting to confirm current requirements and apply manually.

Local capacity is 50 companies, 1,000 jobs, 300 updates, 50 advice notes, and a 16 MiB total store. The student profile has an 8 KiB combined UTF-8 limit. Reaching a limit fails visibly without deleting old evidence. Deleting a company removes its jobs, updates, and entire saved advice notes that cite that company. Backups can retain deleted data.

## Optional OpenRouter usage

In Companies, open the optional usage controls. Supply your own OpenRouter API key, choose **Save key**, then **Fetch daily usage**. Saving never fetches. The write-only field clears after an attempted save; an empty field preserves the saved key unless **Remove saved key** is selected.

The key is plaintext in the private local `store.json` and its backups, matching existing saved-key behavior. It is excluded from API responses, model context, clipboard handoffs, logs, and source distributions. Requests send it only in the Authorization header to the fixed `https://openrouter.ai/api/v1/datasets/rankings-daily` endpoint. No key is sent to job/news sites. Removing it cannot erase earlier backups or filesystem snapshots.

The request covers the seven completed UTC dates before today, excluding the current partial day. Up to 51 rows per day retain exact decimal token strings, dataset version, UTC window, `as_of`, retrieval time, and source. Failed, rate-limited, or invalid responses preserve the previous snapshot; there is no automatic retry. User-entered model prefixes are labels, not verified provider/company ownership; aggregate `other` is never assigned to a company. This dataset measures traffic represented by OpenRouter's dataset, not employer hiring strength or global market share. It is excluded from advice context.

Attribution: **Source: OpenRouter (openrouter.ai/rankings), as of the displayed dataset timestamp. Licensed under CC BY 4.0.** See [OpenRouter rankings](https://openrouter.ai/rankings), [Data API documentation](https://openrouter.ai/docs/cookbook/administration/data-api), and [CC BY 4.0](https://creativecommons.org/licenses/by/4.0/). Preserve attribution when sharing dataset-derived results.

## Review before asking an assistant

Configure and save the assistant in **Settings**, including its existing locality/CLI consent. Save any student edits, select up to six job/update records, and enter a question of at most 2 KiB. Student inclusion defaults off; explicitly select **Include student profile** to attach it. With no selected evidence, the preview says general guidance.

Choose **Preview context** to inspect the exact messages, selected source citations, assistant identity, and delivery destination. Context contains selected evidence/company names, your question, and the profile only when opted in. It excludes keys, usage rows, prior advice, diagnostics, unselected records, company settings, and refresh errors. The final serialized messages are bounded to 30 KiB; excerpt shortening is disclosed, while oversized metadata asks you to narrow the selection. Source text is treated as untrusted data, though this cannot guarantee a model will resist misleading content.

After review, confirm **Send this reviewed context**, then **Analyze**. Ollama/compatible mode sends to the configured loopback runtime; confirm that runtime does not forward elsewhere. Claude Code sends through your installed CLI and its configured provider, subject to its account, policies, and hooks. Codex uses **Copy reviewed context** for a manual clipboard handoff into your own client; it is not executed automatically. Changing selected evidence, question, profile, or assistant settings invalidates the preview. A canceled request may already have reached its destination or committed a local write; reload local state before retrying an uncertain write.

Analysis stays unsaved until you edit the note and choose **Save note**. Saved notes retain source citations from the current selection and the currently selected, saved assistant identity. You can save a manually pasted or edited note without a prior analysis preview; this does not verify authorship or hiring eligibility. Review salary, sponsorship, dates, and other claims against current primary sources. The assistant's instructions ask for grounded facts and explicit unknowns; they do not prove the output is correct. Copying writes reviewed messages to the system clipboard; receiving apps and clipboard history may retain them.

## Version 2 and recovery

This version creates version-2 stores. On opening a valid version-1 store, it preserves the exact original bytes in the dedicated `store.json.v1.bak`, then atomically writes the migrated store. Later writes keep the normal previous-file `store.json.bak` and do not replace the dedicated version-1 backup. Migration stops if a preexisting dedicated backup conflicts; it never silently overwrites it. An old binary cannot read version 2.

For rollback, stop the app and preserve a separate copy of the entire current data directory first. Verify the dedicated backup is the original valid version-1 JSON, then restore a copy as `store.json` in a separate recovery directory and run the old binary there. Version-2 changes made after migration are absent from that backup; retain the version-2 directory for the current binary. Never delete your only copy. Missing stores with recovery backups and corrupt stores refuse to initialize empty data. See [README recovery guidance](../README.md#data-and-recovery).
