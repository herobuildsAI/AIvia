package app

import (
	"math"
	"slices"
	"time"
	_ "time/tzdata"
)

func rule(id, title string, weight float64, why, next, source string) Finding {
	return Finding{ID: id, Title: title, Weight: weight, Lower: 0, Upper: weight, State: "unknown", Explanation: why, Recommendation: next, Source: source}
}
func bounds(f Finding, lower, upper float64) Finding {
	f.Lower = lower
	f.Upper = upper
	if lower == upper {
		f.State = "observed"
	}
	return f
}
func relevantExits(e Evidence, path string) []Exit {
	out := []Exit{}
	for _, x := range e.Exits {
		if x.Path == path {
			out = append(out, x)
		}
	}
	if path == "browser" || path == "agent" {
		if e.Network {
			for _, family := range []string{"ipv4", "ipv6"} {
				if !slices.ContainsFunc(out, func(x Exit) bool { return x.Family == family }) {
					out = append(out, Exit{Path: path, Family: family, Error: "Not observed"})
				}
			}
		}
	}
	if len(out) == 0 {
		return []Exit{{Path: path, Error: "Not observed"}}
	}
	return out
}
func maxRanges(exits []Exit, fn func(Exit) (float64, float64)) (float64, float64) {
	lo, hi := 0.0, 0.0
	for _, x := range exits {
		l, u := fn(x)
		lo = math.Max(lo, l)
		hi = math.Max(hi, u)
	}
	return lo, hi
}
func Score(e Evidence, p Policy, now time.Time) Report {
	p = resolvePolicy(p)
	browser, agent := relevantExits(e, "browser"), relevantExits(e, "agent")
	all := append(append([]Exit{}, browser...), agent...)
	for _, x := range e.Exits {
		if x.Path == "webrtc" {
			all = append(all, x)
		}
	}
	// An omitted agent probe is irrelevant to provider flags if no agent probe was declared.
	if !e.Network {
		all = append([]Exit{}, e.Exits...)
		if len(all) == 0 {
			all = []Exit{{}}
		}
	}
	findings := []Finding{}
	region := rule("region", "Service region policy", 40, "Checks observed browser exits against the selected policy. Missing exits, stale rules, and subregion uncertainty remain unknown.", "Review the service's official policy and the policy date.", p.Source)
	lo, hi := maxRanges(browser, func(x Exit) (float64, float64) {
		if x.IP == "" || x.Intel == nil {
			return 0, 40
		}
		return regionRange(x.Intel.Country, x.Intel.Region, p, now)
	})
	region = bounds(region, lo, hi)
	if lo == 40 {
		region.Explanation = "An observed browser exit is outside the selected supported-region policy. This is separate from account eligibility."
	}
	findings = append(findings, region)
	flags := []struct {
		id, title string
		weight    float64
		get       func(*Intelligence) *bool
	}{
		{"abuse", "Reported abuse association", 15, func(v *Intelligence) *bool { return v.Abuse }},
		{"tor", "Reported Tor exit", 4, func(v *Intelligence) *bool { return v.Tor }},
		{"proxy", "Reported proxy exit", 3, func(v *Intelligence) *bool { return v.Proxy }},
		{"vpn", "Reported VPN exit", 2, func(v *Intelligence) *bool { return v.VPN }},
		{"datacenter", "Reported hosting network", 1, func(v *Intelligence) *bool { return v.Datacenter }},
	}
	ipSource := e.IPSource
	if ipSource == "" {
		ipSource = "IP intelligence not collected"
	}
	for _, v := range flags {
		f := rule(v.id, v.title, v.weight, "A provider flag is one source's observation, not proof of misuse or a ban. Missing data is not a negative result.", "Enable IP intelligence with your own key to check this signal.", ipSource)
		l, u := maxRanges(all, func(x Exit) (float64, float64) {
			if x.IP == "" || x.Intel == nil || v.get(x.Intel) == nil {
				return 0, v.weight
			}
			if *v.get(x.Intel) {
				return v.weight, v.weight
			}
			return 0, 0
		})
		findings = append(findings, bounds(f, l, u))
	}
	f := rule("egress", "Browser and agent egress", 10, "Different exits can indicate split routing or multiple egress nodes; they do not alone prove a leak.", "Check whether browser and command-line traffic use the intended VPN or proxy.", "Observed HTTPS requests")
	lo, hi = maxRanges(browser, func(b Exit) (float64, float64) {
		a, ok := findExit(agent, b.Family)
		if !ok || b.IP == "" || a.IP == "" {
			return 0, 10
		}
		return compareExits(b, a, 10)
	})
	findings = append(findings, bounds(f, lo, hi))
	f = rule("ipv6", "IPv4 and IPv6 region consistency", 8, "Both address families need successful country evidence. An IPv6 timeout does not prove IPv6 is disabled.", "Review IPv6 routing and rerun the browser probes.", "Observed browser HTTPS requests")
	b4, ok4 := findExit(browser, "ipv4")
	b6, ok6 := findExit(browser, "ipv6")
	if ok4 && ok6 && country(b4) != "" && country(b6) != "" {
		v := 0.0
		if country(b4) != country(b6) {
			v = 8
		}
		f = bounds(f, v, v)
	}
	findings = append(findings, f)
	f = rule("webrtc", "WebRTC public egress", 7, "Only validated public candidates are compared. No public candidate means this check is inconclusive.", "Optionally run the STUN check; UDP may use a different route.", "Observed WebRTC candidates")
	if e.WebRTCChecked {
		rtc := relevantExits(e, "webrtc")
		lo, hi = maxRanges(rtc, func(x Exit) (float64, float64) {
			b, ok := findExit(browser, x.Family)
			if !ok || x.IP == "" || b.IP == "" {
				return 0, 7
			}
			return compareExits(x, b, 7)
		})
		f = bounds(f, lo, hi)
	}
	findings = append(findings, f)
	f = rule("timezone-ip", "Browser and IP timezone offset", 4, "A timezone difference is a weak consistency signal, including during travel or across multi-timezone countries.", "Check your intended timezone; do not assume language or timezone alone blocks registration.", "Browser timezone and provider timezone")
	lo, hi = maxRanges(browser, func(x Exit) (float64, float64) {
		if e.Browser.OffsetMinutes == nil || x.Intel == nil || x.Intel.Timezone == "" {
			return 0, 4
		}
		loc, err := time.LoadLocation(x.Intel.Timezone)
		if err != nil {
			return 0, 4
		}
		_, offset := now.In(loc).Zone()
		if offset/60 != *e.Browser.OffsetMinutes {
			return 4, 4
		}
		return 0, 0
	})
	findings = append(findings, bounds(f, lo, hi))
	f = rule("timezone-system", "System and browser timezone offset", 3, "Timezone offsets are compared at the time of the scan. This is weak evidence, not a service rule.", "Check the browser and operating system timezone settings.", "Local system and browser")
	if e.Browser.OffsetMinutes != nil && e.System.OffsetMinutes != nil {
		v := 0.0
		if *e.Browser.OffsetMinutes != *e.System.OffsetMinutes {
			v = 3
		}
		f = bounds(f, v, v)
	}
	findings = append(findings, f)
	f = rule("clock", "System clock consistency", 3, "Requires two independent, consistent HTTPS Date observations. Network failures leave the check unknown.", "Verify automatic date and time if there is a confirmed large offset.", "Independent HTTPS response dates")
	if e.ClockSkewSeconds != nil {
		v := 0.0
		if math.Abs(*e.ClockSkewSeconds) > 300 {
			v = 3
		}
		f = bounds(f, v, v)
	}
	findings = append(findings, f)
	lower, upper, resolved := 0.0, 0.0, 0.0
	for _, f := range findings {
		lower += f.Lower
		upper += f.Upper
		if f.State == "observed" {
			resolved += f.Weight
		}
	}
	grade := "Insufficient evidence to classify"
	if band(lower) == band(upper) {
		grade = band(lower)
	}
	r := RedactedReport{ID: newID(), CreatedAt: now.UTC().Format(time.RFC3339), ScoreVersion: "1.0", Policy: p, Lower: lower, Upper: upper, Coverage: resolved, Grade: grade, Findings: findings, OS: e.System.OS, Arch: e.System.Arch, Timezone: e.System.Timezone, Target: e.Target}
	return Report{RedactedReport: r, Evidence: e}
}
func findExit(exits []Exit, family string) (Exit, bool) {
	for _, x := range exits {
		if x.Family == family {
			return x, true
		}
	}
	return Exit{}, false
}
func country(x Exit) string {
	if x.IP != "" && x.Intel != nil && validCountry(x.Intel.Country) {
		return x.Intel.Country
	}
	return ""
}
func compareExits(a, b Exit, weight float64) (float64, float64) {
	left, right := canonicalIP(a.IP), canonicalIP(b.IP)
	if left == "" || right == "" {
		return 0, weight
	}
	if left == right {
		return 0, 0
	}
	ca, cb := country(a), country(b)
	if ca == "" || cb == "" {
		return weight / 2, weight
	}
	if ca == cb {
		return weight / 2, weight / 2
	}
	return weight, weight
}
func band(n float64) string {
	if n < 20 {
		return "No strong signals observed"
	}
	if n < 40 {
		return "Some risk signals"
	}
	if n < 70 {
		return "Elevated"
	}
	return "High"
}
func Redact(r Report) RedactedReport {
	// A projection, rather than regex masking arbitrary provider text, keeps new raw fields out by default.
	out := r.RedactedReport
	out.Findings = append([]Finding{}, out.Findings...)
	return out
}
