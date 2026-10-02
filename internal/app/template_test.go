package app

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestServiceTemplateRoundTrip(t *testing.T) {
	s, store := testServer(t)
	p, err := store.SaveProfile(Profile{Name: "Claude", Kind: "website", Origin: "https://claude.ai", Notes: "PRIVATE-NOTES", Status: "blocked", Policy: Policy{Mode: "builtin", Builtin: "claude-web"}, Issues: []Issue{{Stage: "login", Error: "PRIVATE-ERROR"}}})
	if err != nil {
		t.Fatal(err)
	}
	w := apiTest(s, "GET", "/api/profiles/"+p.ID+"/template", "")
	if w.Code != 200 {
		t.Fatalf("export: %d %s", w.Code, w.Body)
	}
	for _, private := range []string{p.ID, "PRIVATE-NOTES", "PRIVATE-ERROR", `"issues"`, `"reports"`, `"status"`, `"provenance"`, `"builtin"`} {
		if strings.Contains(w.Body.String(), private) {
			t.Fatalf("template contains %s", private)
		}
	}
	preview := apiTest(s, "POST", "/api/templates/preview", w.Body.String())
	if preview.Code != 200 {
		t.Fatalf("preview: %d %s", preview.Code, preview.Body)
	}
	var draft Profile
	if err = json.Unmarshal(preview.Body.Bytes(), &draft); err != nil {
		t.Fatal(err)
	}
	if draft.ID != "" || draft.Revision != 0 || draft.Status != "unchecked" || draft.Notes != "" || len(draft.Issues) != 0 || len(store.Snapshot().Profiles) != 1 {
		t.Fatalf("preview modified state or retained private identity: %#v", draft)
	}
	if draft.Policy.Mode != "custom" || draft.Policy.CheckedAt != "2026-10-02" || len(draft.Policy.Countries) == 0 || len(draft.Policy.ExcludedRegions["UA"]) != 5 {
		t.Fatal("starter snapshot lost source/date/exclusions")
	}
	if resolvePolicy(draft.Policy).Provenance != "User-provided, not independently verified" {
		t.Fatal("import incorrectly trusted")
	}
	copy, err := store.SaveProfile(draft)
	if err != nil || copy.ID == p.ID || len(store.Snapshot().Profiles) != 2 {
		t.Fatal("import must create a new service")
	}
	if original := store.Snapshot().Profiles[0]; original.Notes != "PRIVATE-NOTES" || original.Revision != p.Revision {
		t.Fatal("original service changed")
	}
}

func TestServiceTemplateUntrustedInput(t *testing.T) {
	s, store := testServer(t)
	base := `{"format":"aivia-service-template","version":1,"name":"Example","kind":"website","origin":"https://example.com","policy":{"mode":"custom","source":"https://example.com/regions","checkedAt":"2026-01-01","countries":["US"]}}`
	w := apiTest(s, "POST", "/api/templates/preview", base)
	if w.Code != 200 {
		t.Fatalf("valid template: %d %s", w.Code, w.Body)
	}
	var draft Profile
	_ = json.Unmarshal(w.Body.Bytes(), &draft)
	lo, hi := regionRange("US", "", draft.Policy, scoreTime)
	if draft.Policy.CheckedAt != "2026-01-01" || lo != 0 || hi != 40 {
		t.Fatal("stale imported policy became current")
	}
	for _, raw := range []string{
		strings.Replace(base, `"version":1`, `"version":2`, 1),
		strings.Replace(base, `"name":"Example"`, `"name":"Example","notes":"secret"`, 1),
		strings.Replace(base, `"mode":"custom"`, `"mode":"builtin","builtin":"claude-web"`, 1),
		strings.Replace(base, `"mode":"custom"`, `"mode":"custom","provenance":"Official"`, 1),
		strings.Replace(base, `"US"`, `"ZZ"`, 1),
		strings.Replace(base, "https://example.com/regions", "https://127.0.0.1/regions", 1),
		strings.Replace(base, "https://example.com/regions", "https://example.com/regions?token=secret", 1),
		strings.Replace(base, "https://example.com/regions", "https://example.com:8443/regions", 1),
		strings.Replace(base, `"mode":"custom"`, `"mode":"worldwide"`, 1),
		strings.Replace(base, `"countries":["US"]`, `"countries":["US"],"excludedRegions":{"US":["Region, Other"]}`, 1),
		strings.Replace(base, `"countries":["US"]`, `"countries":["US"],"excludedRegions":{"US":["Region\nOther"]}`, 1),
		strings.Repeat(" ", 257<<10) + base,
		base + `{}`,
	} {
		if got := apiTest(s, "POST", "/api/templates/preview", raw); got.Code != 400 {
			t.Errorf("invalid template accepted: status %d", got.Code)
		}
	}
	if len(store.Snapshot().Profiles) != 0 {
		t.Fatal("preview saved a service")
	}
}

func TestServiceTemplateUnknownDoesNotInventRules(t *testing.T) {
	s, store := testServer(t)
	p, _ := store.SaveProfile(Profile{Name: "Muse", Kind: "app", Origin: "https://muse.ai", Policy: Policy{Mode: "unknown"}})
	w := apiTest(s, "GET", "/api/profiles/"+p.ID+"/template", "")
	if w.Code != 200 {
		t.Fatalf("export: %d", w.Code)
	}
	w = apiTest(s, "POST", "/api/templates/preview", w.Body.String())
	var draft Profile
	_ = json.Unmarshal(w.Body.Bytes(), &draft)
	if w.Code != 200 || draft.Policy.Mode != "unknown" || len(draft.Policy.Countries) != 0 || draft.Policy.Source != "" {
		t.Fatal("name or origin inferred a policy")
	}
}
