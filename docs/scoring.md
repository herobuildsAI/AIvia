# Risk index v1.0

The score is a transparent diagnostic heuristic, not a calibrated probability or an IP-cleanliness certification. Higher numbers indicate more adverse signals. Language, operating-system name, and VPN use alone do not establish a registration restriction.

Each rule returns an interval `[lower, upper]`. A missing observation contributes `[0, weight]`; a confirmed non-adverse observation contributes `[0, 0]`; a confirmed adverse observation contributes `[weight, weight]`. Multiple applicable exits are combined by the maximum lower and upper contribution, preventing duplicate counting within a rule. Rule contributions are summed; weights total 100.

| Rule | Weight | Evidence |
| --- | ---: | --- |
| Selected service region policy | 40 | Browser exit countries, current policy snapshot, subregion exclusions |
| Reported abuse association | 15 | Explicit provider flag |
| Reported Tor exit | 4 | Explicit provider flag |
| Reported proxy exit | 3 | Explicit provider flag |
| Reported VPN exit | 2 | Explicit provider flag |
| Reported hosting network | 1 | Explicit provider flag |
| Browser/agent egress mismatch | 10 | Same-family observed addresses and countries |
| IPv4/IPv6 region mismatch | 8 | Both browser families and countries |
| WebRTC public egress mismatch | 7 | Public candidates versus same-family browser HTTPS exits |
| Browser/IP timezone offset | 4 | Browser offset and provider IANA timezone at scan time |
| System/browser timezone offset | 3 | Observed current offsets |
| Clock discrepancy | 3 | Two consistent HTTPS Date samples; adverse if over five minutes |

For browser/agent and WebRTC comparisons, equal IPs contribute zero. IPv6 expansion, letter case, and IPv4-mapped IPv6 spellings are normalized before comparison and provider matching; alternate spellings cannot create a mismatch or duplicate a lookup. Different IPs in the same known country contribute half the weight. Different countries contribute the full weight. Different IPs without sufficient country evidence contribute `[weight/2, weight]`. An IPv6 timeout and absent WebRTC candidate remain unknown, not a pass.

Evidence coverage is the sum of weights whose intervals are fully resolved. Partial intervals do not count as resolved. The grade is shown only if the entire range falls in one band: below 20, No strong signals observed; 20–39.9, Some risk signals; 40–69.9, Elevated; 70–100, High. Otherwise it is Insufficient evidence to classify.

Policies expire after 30 days. Unknown dates, future dates, unknown country codes, or unresolved regional exclusions widen uncertainty. Claude's Ukraine exclusions remain uncertain unless the provider explicitly identifies a listed excluded region. User policies are marked user-provided, not independently verified. An explicit worldwide policy still requires source/date and usable country evidence.

HTTP status codes, DNS configuration, and locale are informational. A 200 does not prove eligibility, and a 403 does not establish a country restriction. No account outcome is inferred from these alone. Official service rules and account-specific support remain authoritative.

## Reading results and next checks

The report separates findings with a positive lower bound (an observed scored signal), zero lower bound with positive upper bound (unknown), and zero upper bound (no adverse signal in that check). A partly resolved positive signal stays in the first group with an explicit uncertainty label. These groups are a presentation of the existing score, not additional evidence. A zero lower bound with unresolved points does not establish low risk; even a fully resolved zero does not establish account eligibility. Coverage measures resolved rule weights, not statistical confidence.

The next-check list shows up to three steps: positive signals ordered by their lower-bound contribution, followed by unknowns ordered by their upper bound. IP-provider flags, routing observations, and time observations each share a step to avoid duplicate advice. The displayed points belong to the named representative finding, not a combined group score. All individual findings remain available below. Source and settings shortcuts only navigate; users still review sources and explicitly opt in before external checks or assistant calls. Saved reports use the same presentation without changing stored scores or raw evidence retention.

## Comparing checks

Diagnostics can compare the selected snapshot with an earlier saved or in-memory snapshot of the same service. Findings are matched by their IDs, not array order. Different scoring versions are not compared. The view shows changes in ranges, coverage, finding states, and target reachability; it warns when policy, service revision, or evidence source differs. A missing finding remains unknown. A lower score with less evidence does not establish an improvement, and before/after observations do not establish causation. Saved snapshots exclude raw IP addresses, so the comparison cannot reconstruct old exit addresses or verify which VPN node was used.
