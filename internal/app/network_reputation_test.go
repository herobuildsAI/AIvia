package app

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func reputationIntel(t *testing.T, ip string, company, asn any) *Intelligence {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"ip": ip, "is_datacenter": true, "is_abuser": false, "location": map[string]string{"country_code": "US"}, "company": company, "asn": asn})
	got, err := parseIntelligence(b, ip)
	if err != nil {
		t.Fatal(err)
	}
	return got
}
func intelFields(t *testing.T, v *Intelligence) map[string]any {
	t.Helper()
	b, _ := json.Marshal(v)
	var fields map[string]any
	json.Unmarshal(b, &fields)
	return fields
}
func TestNetworkMetadataValidation(t *testing.T) {
	for _, tc := range []struct{ ip, network, route, wantNetwork, wantRoute string }{
		{"8.8.8.8", "8.8.8.0 - 8.8.8.255", "8.8.8.0/24", "8.8.8.0 - 8.8.8.255", "8.8.8.0/24"},
		{"2606:4700:4700::1111", "2606:4700:4700::/48", "2606:4700::/32", "2606:4700:4700::/48", "2606:4700::/32"},
		{"2606:4700:4700::1111", "2606:4700:4700:: - 2606:4700:4700::ffff", "2606:4700:4700::/48", "2606:4700:4700:: - 2606:4700:4700::ffff", "2606:4700:4700::/48"},
		{"8.8.8.8", "1.1.1.0/24", "1.1.1.0/24", "", ""},
		{"8.8.8.8", "8.8.8.8/24", "8.8.8.8/24", "", ""},
		{"8.8.8.8", "8.8.8.255 - 8.8.8.0", "bad", "", ""},
		{"8.8.8.8", "::/0", "::/0", "", ""},
	} {
		t.Run(tc.network, func(t *testing.T) {
			got := reputationIntel(t, tc.ip, map[string]any{"name": "<script>org</script>", "type": "hosting", "network": tc.network, "abuser_score": "0 (High)"}, map[string]any{"asn": 15169, "org": "Example ASN", "type": "isp", "route": tc.route, "abuser_score": "0.1 (Low)"})
			fields := intelFields(t, got)
			if fields["companyNetwork"] != tc.wantNetwork || fields["asnRoute"] != tc.wantRoute {
				t.Fatalf("network validation: %#v", fields)
			}
			if (fields["companyAbuseRatio"] != nil) != (tc.wantNetwork != "") || (fields["asnAbuseRatio"] != nil) != (tc.wantRoute != "") {
				t.Fatalf("unscoped ratio: %#v", fields)
			}
			if tc.wantNetwork != "" && fields["companyAbuseRatio"] != float64(0) {
				t.Fatal("numeric zero lost")
			}
			if got.Country != "US" || got.Abuse == nil || *got.Abuse {
				t.Fatal("metadata damaged country/flags")
			}
		})
	}
}
func TestOptionalReputationMalformedStaysUnknown(t *testing.T) {
	for _, ratio := range []any{nil, true, 0, []any{}, map[string]any{}, "", "garbage", "0.1 garbage", "-0.1", "1.1", "NaN", "Inf", strings.Repeat("1", 161)} {
		got := reputationIntel(t, "8.8.8.8", map[string]any{"network": "8.8.8.0/24", "abuser_score": ratio}, map[string]any{"route": "8.8.8.0/24", "abuser_score": ratio})
		fields := intelFields(t, got)
		if fields["companyAbuseRatio"] != nil || fields["asnAbuseRatio"] != nil || got.Country != "US" {
			t.Fatalf("invalid ratio accepted: %v %#v", ratio, fields)
		}
	}
	for _, optional := range []any{"wrong", true, 42, []any{}, map[string]any{"asn": "bad", "org": true, "name": strings.Repeat("a", 161), "type": false, "network": 3, "route": true, "abuser_score": "0.1"}} {
		got := reputationIntel(t, "8.8.8.8", optional, optional)
		fields := intelFields(t, got)
		if fields["companyAbuseRatio"] != nil || fields["asnAbuseRatio"] != nil || got.Country != "US" || fields["companyName"] != "" {
			t.Fatalf("optional field corrupted base evidence: %#v", fields)
		}
	}
}
func TestScoreNetworkReputationBandsAndUncertainty(t *testing.T) {
	for _, tc := range []struct {
		ratio  any
		lo, hi float64
	}{
		{nil, 0, 5}, {"0 (Very High)", 0, 0}, {"0.0001 (Low)", 1, 1}, {"0.0099", 1, 1}, {"0.01", 3, 3}, {"0.0999", 3, 3}, {"0.10 (Low)", 5, 5}, {"1", 5, 5},
	} {
		intel := reputationIntel(t, "8.8.8.8", map[string]any{"network": "8.8.8.0/24", "abuser_score": tc.ratio}, map[string]any{"route": "8.8.8.0/24", "abuser_score": tc.ratio})
		r := Score(Evidence{Exits: []Exit{{Path: "browser", IP: "8.8.8.8", Intel: intel}}}, Policy{Mode: "unknown"}, scoreTime)
		f := finding(t, r, "network-reputation")
		if f.Lower != tc.lo || f.Upper != tc.hi {
			t.Fatalf("%v: %#v", tc.ratio, f)
		}
		if r.ScoreVersion != "1.1" || finding(t, r, "datacenter").Lower != 4 || strings.Contains(finding(t, r, "datacenter").Explanation, "confirmed blocked") {
			t.Fatal("version or hosting interpretation wrong")
		}
		sum := 0.0
		for _, f := range r.Findings {
			sum += f.Weight
		}
		if sum != 100 {
			t.Fatalf("weights %v", sum)
		}
	}
	for _, tc := range []struct {
		company, asn any
		lo, hi       float64
	}{{"0.01", nil, 3, 5}, {nil, "0", 0, 5}, {"0.1", nil, 5, 5}, {"0.01", "0.01", 3, 3}, {"0", "0", 0, 0}} {
		intel := reputationIntel(t, "8.8.8.8", map[string]any{"network": "8.8.8.0/24", "abuser_score": tc.company}, map[string]any{"route": "8.8.8.0/24", "abuser_score": tc.asn})
		e := Evidence{Exits: []Exit{{Path: "browser", IP: "8.8.8.8", Intel: intel}, {Path: "agent", IP: "8.8.8.8", Intel: intel}}}
		f := finding(t, Score(e, Policy{Mode: "unknown"}, scoreTime), "network-reputation")
		if f.Lower != tc.lo || f.Upper != tc.hi {
			t.Fatalf("max semantics: %#v", f)
		}
		e.Network = true
		f = finding(t, Score(e, Policy{Mode: "unknown"}, scoreTime), "network-reputation")
		if f.Lower != tc.lo || f.Upper != 5 {
			t.Fatalf("missing families erased: %#v", f)
		}
	}
}
func TestNetworkMetadataRedaction(t *testing.T) {
	intel := reputationIntel(t, "8.8.8.8", map[string]any{"name": "PRIVATE-COMPANY", "type": "hosting", "network": "8.8.8.0/24", "abuser_score": "0.01"}, map[string]any{"asn": 15169, "org": "PRIVATE-ASN", "type": "isp", "route": "8.8.8.0/24", "abuser_score": "0.1"})
	r := Score(Evidence{Exits: []Exit{{IP: "8.8.8.8", Intel: intel}}}, Policy{Mode: "unknown"}, scoreTime)
	b, _ := json.Marshal(Redact(r))
	contextText, err := reportContext(Redact(r), "")
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{"PRIVATE-COMPANY", "PRIVATE-ASN", "15169", "8.8.8.0/24", "8.8.8.8", "companyAbuseRatio", "asnAbuseRatio"} {
		if strings.Contains(string(b), raw) || strings.Contains(contextText, raw) {
			t.Fatalf("redaction leaked %s", raw)
		}
	}
}
func TestFetchAIviaUserAgent(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != "AIvia/0.1" {
			t.Errorf("wrong user agent %q", r.Header.Get("User-Agent"))
		}
		w.Write([]byte("ok"))
	}))
	defer ts.Close()
	if _, _, err := fetch(context.Background(), ts.Client(), http.MethodGet, ts.URL, nil, 100); err != nil {
		t.Fatal(err)
	}
}

func TestScoreRequiresScopedReputation(t *testing.T) {
	for _, tc := range []struct {
		network string
		ratio   float64
	}{
		{"1.1.1.0/24", 0.1}, {"", 0.1}, {"8.8.8.0/24", math.NaN()}, {"8.8.8.0/24", math.Inf(1)}, {"8.8.8.0/24", -0.1}, {"8.8.8.0/24", 1.1},
	} {
		intel := &Intelligence{CompanyNetwork: tc.network, ASNRoute: tc.network, CompanyAbuseRatio: ptr(tc.ratio), ASNAbuseRatio: ptr(tc.ratio)}
		f := finding(t, Score(Evidence{Exits: []Exit{{IP: "8.8.8.8", Intel: intel}}}, Policy{Mode: "unknown"}, scoreTime), "network-reputation")
		if f.Lower != 0 || f.Upper != 5 {
			t.Fatalf("invalid normalized attribution accepted: %#v", f)
		}
	}
}
func TestScoreWorstExitAndExplicitHosting(t *testing.T) {
	first := reputationIntel(t, "8.8.8.8", map[string]any{"type": "hosting", "network": "8.8.8.0/24", "abuser_score": "0.01"}, map[string]any{"route": "8.8.8.0/24", "abuser_score": "0"})
	second := reputationIntel(t, "1.1.1.1", map[string]any{"network": "1.1.1.0/24", "abuser_score": "0.001"}, map[string]any{"route": "1.1.1.0/24", "abuser_score": "0.01"})
	e := Evidence{Exits: []Exit{{IP: "8.8.8.8", Intel: first}, {IP: "1.1.1.1", Intel: second}}}
	f := finding(t, Score(e, Policy{Mode: "unknown"}, scoreTime), "network-reputation")
	if f.Lower != 3 || f.Upper != 3 {
		t.Fatalf("company/ASN/exits added together: %#v", f)
	}
	first.Datacenter = ptr(false)
	second.Datacenter = ptr(false)
	f = finding(t, Score(e, Policy{Mode: "unknown"}, scoreTime), "datacenter")
	if f.Lower != 0 || f.Upper != 0 {
		t.Fatal("inferred hosting despite explicit false flag")
	}
	first.Datacenter = nil
	second.Datacenter = nil
	f = finding(t, Score(e, Policy{Mode: "unknown"}, scoreTime), "datacenter")
	if f.Lower != 0 || f.Upper != 4 {
		t.Fatal("inferred hosting from company type")
	}
}
