package app

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

var scoreTime = time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)

func ptr[T any](v T) *T { return &v }
func finding(t *testing.T, r Report, id string) Finding {
	t.Helper()
	for _, f := range r.Findings {
		if f.ID == id {
			return f
		}
	}
	t.Fatalf("missing finding %s", id)
	return Finding{}
}
func TestScoreUnknown(t *testing.T) {
	r := Score(Evidence{}, Policy{Mode: "unknown"}, scoreTime)
	if r.Lower != 0 || r.Upper != 100 || r.Coverage != 0 {
		t.Fatalf("unknown became certain: %#v", r.RedactedReport)
	}
}
func TestScoreLocalOffset(t *testing.T) {
	r := Score(Evidence{Browser: BrowserEvidence{OffsetMinutes: ptr(0)}, System: SystemEvidence{OffsetMinutes: ptr(0)}}, Policy{Mode: "unknown"}, scoreTime)
	if r.Upper != 97 || r.Coverage != 3 {
		t.Fatalf("offset coverage %#v", r.RedactedReport)
	}
}
func TestScorePartialPaths(t *testing.T) {
	e := Evidence{Network: true, Exits: []Exit{{Path: "browser", Family: "ipv4", IP: "8.8.8.8"}, {Path: "agent", Family: "ipv4", IP: "1.1.1.1"}}}
	r := Score(e, Policy{Mode: "unknown"}, scoreTime)
	f := finding(t, r, "egress")
	if f.Lower != 5 || f.Upper != 10 {
		t.Fatalf("partial mismatch %#v", f)
	}
	e.Exits = append(e.Exits, Exit{Path: "browser", Family: "ipv6", Error: "timeout"}, Exit{Path: "agent", Family: "ipv6", Error: "timeout"})
	f = finding(t, Score(e, Policy{Mode: "unknown"}, scoreTime), "ipv6")
	if f.Upper != 8 {
		t.Fatal("failed IPv6 treated as safe")
	}
}
func TestScorePolicyAndMissingFlags(t *testing.T) {
	e := Evidence{Exits: []Exit{{Path: "browser", Family: "ipv4", IP: "8.8.8.8", Intel: &Intelligence{Country: "CN", Abuse: ptr(false)}}}}
	r := Score(e, Policy{Mode: "builtin", Builtin: "claude-web"}, scoreTime)
	if finding(t, r, "region").Lower != 40 {
		t.Fatal("unsupported region hidden")
	}
	if finding(t, r, "abuse").Upper != 0 || finding(t, r, "vpn").Upper != 1 {
		t.Fatal("false and absent flags confused")
	}
	r = Score(e, Policy{Mode: "builtin", Builtin: "claude-web"}, scoreTime.AddDate(0, 0, 31))
	if finding(t, r, "region").Lower != 0 || finding(t, r, "region").Upper != 40 {
		t.Fatal("stale rule accepted")
	}
	e.Exits[0].Intel.Country = "UA"
	r = Score(e, Policy{Mode: "builtin", Builtin: "claude-web"}, scoreTime)
	if finding(t, r, "region").State != "unknown" {
		t.Fatal("subregion uncertainty hidden")
	}
}
func TestScoreMissingExitNotErased(t *testing.T) {
	e := Evidence{Exits: []Exit{{Path: "browser", Family: "ipv4", IP: "8.8.8.8", Intel: &Intelligence{Country: "US", VPN: ptr(false)}}, {Path: "browser", Family: "ipv6", Error: "timeout"}}}
	r := Score(e, Policy{Mode: "builtin", Builtin: "claude-web"}, scoreTime)
	if finding(t, r, "vpn").Upper != 1 || finding(t, r, "region").Upper != 40 {
		t.Fatal("successful exit erased failed probe")
	}
}
func TestScoreWebRTCFraction(t *testing.T) {
	e := Evidence{WebRTCChecked: true, Exits: []Exit{{Path: "browser", Family: "ipv4", IP: "8.8.8.8", Intel: &Intelligence{Country: "US"}}, {Path: "webrtc", Family: "ipv4", IP: "1.1.1.1", Intel: &Intelligence{Country: "US"}}}}
	f := finding(t, Score(e, Policy{Mode: "unknown"}, scoreTime), "webrtc")
	if f.Lower != 3.5 || f.Upper != 3.5 {
		t.Fatalf("fraction lost: %#v", f)
	}
}
func TestRedact(t *testing.T) {
	r := Score(Evidence{Exits: []Exit{{Path: "browser", IP: "8.8.8.8", Intel: &Intelligence{ASN: "PRIVATE-ROW"}}}}, Policy{Mode: "unknown"}, scoreTime)
	b, _ := json.Marshal(Redact(r))
	for _, v := range []string{"8.8.8.8", "PRIVATE-ROW", `"exits":`, `"intelligence":`} {
		if strings.Contains(string(b), v) {
			t.Fatalf("leaked %s", v)
		}
	}
}
func TestPolicyInvalidCodes(t *testing.T) {
	if err := validatePolicy(Policy{Mode: "custom", Source: "https://example.com/policy", CheckedAt: "2026-10-02", Countries: []string{"ZZ"}}); err == nil {
		t.Fatal("non-country code accepted")
	}
}

func TestScoreEquivalentAddresses(t *testing.T) {
	for _, pair := range [][2]string{
		{"2606:4700:4700::1111", "2606:4700:4700:0:0:0:0:1111"},
		{"2001:4860:4860::ABCD", "2001:4860:4860::abcd"},
		{"8.8.8.8", "::ffff:8.8.8.8"},
	} {
		e := Evidence{WebRTCChecked: true, Exits: []Exit{
			{Path: "browser", Family: familyOf(pair[0]), IP: pair[0]},
			{Path: "agent", Family: familyOf(pair[1]), IP: pair[1]},
			{Path: "webrtc", Family: familyOf(pair[1]), IP: pair[1]},
		}}
		for _, id := range []string{"egress", "webrtc"} {
			f := finding(t, Score(e, Policy{Mode: "unknown"}, scoreTime), id)
			if f.Lower != 0 || f.Upper != 0 {
				t.Errorf("equivalent addresses penalized for %s: %s / %s => %v–%v", id, pair[0], pair[1], f.Lower, f.Upper)
			}
		}
	}
}

func TestCustomIPSourceSurvivesRedaction(t *testing.T) {
	r := Score(Evidence{IPSource: "User-configured IP intelligence provider"}, Policy{Mode: "unknown"}, scoreTime)
	for _, f := range Redact(r).Findings {
		if f.ID == "vpn" && f.Source != "User-configured IP intelligence provider" {
			t.Fatal("custom evidence incorrectly attributed to built-in provider")
		}
	}
}
