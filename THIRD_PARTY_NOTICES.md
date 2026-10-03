# Third-party notices

Original code, interface, and documentation are licensed under the MIT License in `LICENSE`.

## Go runtime and standard library

Compiled binaries include the Go runtime and standard library, licensed under the Go BSD-style license. Its full notice is in `LICENSES/Go-BSD-3-Clause.txt`; this source distribution includes it and the bundled Go dependency notices in `LICENSES/Go-vendored-notices.txt`. This repository has no third-party Go modules, npm dependencies, or bundled font files. The `time/tzdata` package embeds timezone data supplied with Go; IANA timezone data is public-domain factual data. See [IANA timezone distribution](https://www.iana.org/time-zones) and [Go time/tzdata](https://pkg.go.dev/time/tzdata). A distributor changing toolchains or dependencies must recheck all applicable notices, including the toolchain's bundled third-party notices.

## External services and factual policy snapshots

Network responses are obtained only for user-requested checks. Access to an API does not grant a blanket right to redistribute its databases. No commercial reputation database is included or relicensed as MIT.

- [ipify](https://www.ipify.org/): public-IP observations.
- [ipapi.is documentation](https://ipapi.is/developers.html) and [terms](https://ipapi.is/terms.html): optional user-keyed IP intelligence; provider quotas and terms apply.
- [Cloudflare](https://www.cloudflare.com/): HTTPS response Date metadata for a second clock observation.
- Optional Google STUN (`stun.l.google.com:19302`), configurable with `-stun`: public WebRTC candidate discovery.
- [Anthropic supported countries](https://www.anthropic.com/supported-countries): dated factual country-code snapshots, independently recorded for Claude web/API on 2026-10-02. No service branding, policy prose, or logos are copied. Rules can change; stale snapshots become unknown.

Country codes are ISO 3166-1 alpha-2 identifiers. No proprietary country database or translated country-name dataset is bundled.

The optional access-research reader fetches public pages at the user's request. Runtime excerpts and generated summaries are not bundled or relicensed as MIT. Publisher content, access terms, and attribution remain separate from this project's source-code license. Known Claude source suggestions link to the supported-regions page, Claude Help Center, and Claude Status; source text is retrieved at runtime, not redistributed in releases.

## Local model runtimes and weights

Ollama and compatible runtimes are separately installed software. Model weights are neither bundled nor downloaded by this project. Users and distributors must comply with each selected runtime/model license; the project's MIT license does not replace those terms. Names identify interoperability only.


## User-installed command-line assistants

Claude Code and Codex executables, account credentials, and model weights are not bundled or redistributed by this project. The repository's MIT license covers its adapter code, not those products or their services. Users install and authenticate official tools separately. Their applicable software licenses, service terms, administrator policies, and account usage limits remain in force. No subscription credentials are extracted, copied, or repurposed as a provider API.

The compatible IP endpoint option does not supply a database license or redistribution rights. Users supply their own service and authorized data source.

## Career jobs, news, and optional usage data

Greenhouse, Ashby, and Lever names identify compatible public recruiting adapters only; no affiliation is implied. Job postings, company pages, RSS/Atom items, runtime excerpts, and pasted third-party text are not bundled or relicensed as MIT. Their publishers' copyright, access terms, and source attribution apply separately. Company association and source selection are user-entered, not independently verified. Generated/user-edited advice does not establish publisher authorship or hiring eligibility. Review the source's terms before sharing its content.

The optional [OpenRouter daily dataset](https://openrouter.ai/docs/cookbook/administration/data-api) is fetched with the user's own key and is licensed under [CC BY 4.0](https://creativecommons.org/licenses/by/4.0/). The UI attributes **Source: OpenRouter (openrouter.ai/rankings), as of the displayed dataset timestamp. Licensed under CC BY 4.0.** Preserve attribution, link to the license, and identify changes when sharing dataset-derived material. The dataset is not bundled, its license is separate from MIT, and its measured traffic does not establish employer rankings or global market share.
