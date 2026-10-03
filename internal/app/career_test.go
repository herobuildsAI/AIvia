package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func careerLegacy(t *testing.T, dir string) []byte {
	t.Helper()
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.SaveProfile(Profile{Name: "Preserved service", Kind: "app", Notes: "service notes"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SaveReport(RedactedReport{ID: newID(), ProfileID: p.ID, ProfileRevision: p.Revision, Lower: 10, Upper: 20}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveSettings(ModelSettings{Provider: "compatible", Endpoint: "http://127.0.0.1:1234/v1", Model: "fixture-model", LocalConfirmed: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveIPSettings(IPSettings{Provider: "custom", Endpoint: "https://example.com/ip", APIKey: "fixture-private-ip-key", Revision: 0}); err != nil {
		t.Fatal(err)
	}
	st := s.Snapshot()
	st.Version = 1
	data, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	delete(fields, "career")
	data, err = json.MarshalIndent(fields, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "store.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	return data
}

func TestCareerMigrationPreservesLegacy(t *testing.T) {
	dir := t.TempDir()
	raw := careerLegacy(t, dir)
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.Snapshot().Version != 2 {
		t.Fatal("legacy store was not migrated to version 2")
	}
	backup, err := os.ReadFile(filepath.Join(dir, "store.json.v1.bak"))
	if err != nil || !bytes.Equal(backup, raw) {
		t.Fatal("exact legacy recovery bytes missing")
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(filepath.Join(dir, "store.json.v1.bak"))
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatal("legacy backup permissions are not private")
		}
	}
	var before State
	if err := json.Unmarshal(raw, &before); err != nil {
		t.Fatal(err)
	}
	after := s.Snapshot()
	if !reflect.DeepEqual(before.Profiles, after.Profiles) || !reflect.DeepEqual(before.Reports, after.Reports) || before.Settings != after.Settings || before.IPSettings != after.IPSettings {
		t.Fatal("migration altered diagnostics data")
	}
	if err := s.SaveSettings(ModelSettings{Provider: "ollama", Endpoint: "http://127.0.0.1:11434", Model: "test"}); err != nil {
		t.Fatal(err)
	}
	backup, _ = os.ReadFile(filepath.Join(dir, "store.json.v1.bak"))
	if !bytes.Equal(backup, raw) {
		t.Fatal("later save altered legacy backup")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if reopened.Snapshot().Version != 2 {
		t.Fatal("version 2 reopen failed")
	}
}

func TestCareerMigrationRecoveryFailures(t *testing.T) {
	for _, kind := range []string{"conflicting-backup", "symlink-backup", "leftover-temp", "career-null", "career-null-case", "invalid-legacy", "future-version", "directory-backup", "unsafe-general-backup"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			raw := careerLegacy(t, dir)
			path := filepath.Join(dir, "store.json")
			backup := path + ".v1.bak"
			switch kind {
			case "conflicting-backup":
				if err := os.WriteFile(backup, []byte("do not replace"), 0600); err != nil {
					t.Fatal(err)
				}
			case "symlink-backup":
				target := filepath.Join(dir, "recovery")
				if err := os.WriteFile(target, raw, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, backup); err != nil {
					t.Skip("symlinks unavailable")
				}
			case "leftover-temp":
				if err := os.WriteFile(path+".tmp", []byte("leftover"), 0600); err != nil {
					t.Fatal(err)
				}
			case "directory-backup":
				if err := os.Mkdir(backup, 0700); err != nil {
					t.Fatal(err)
				}
			case "unsafe-general-backup":
				if err := os.Remove(path + ".bak"); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(path+".bak", 0700); err != nil {
					t.Fatal(err)
				}
			case "career-null-case":
				raw = bytes.Replace(raw, []byte(`"version": 1`), []byte(`"version": 1, "Career": null`), 1)
			case "career-null":
				raw = bytes.Replace(raw, []byte(`"version": 1`), []byte(`"version": 1, "career": null`), 1)
			case "invalid-legacy":
				raw = bytes.Replace(raw, []byte(`"profiles": [`), []byte(`"profiles": [null,`), 1)
			case "future-version":
				raw = bytes.Replace(raw, []byte(`"version": 1`), []byte(`"version": 3`), 1)
			}
			if err := os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
			s, err := OpenStore(dir)
			if err == nil {
				s.Close()
				t.Fatal("unsafe migration accepted")
			}
			got, _ := os.ReadFile(path)
			if !bytes.Equal(got, raw) {
				t.Fatal("migration failure replaced original")
			}
			if _, err := os.Stat(filepath.Join(dir, "store.lock")); !os.IsNotExist(err) {
				t.Fatal("failed migration retained lock")
			}
			if kind == "conflicting-backup" {
				got, _ = os.ReadFile(backup)
				if string(got) != "do not replace" {
					t.Fatal("conflicting backup changed")
				}
			}
		})
	}
}

func TestCareerMigrationIdenticalBackup(t *testing.T) {
	dir := t.TempDir()
	raw := careerLegacy(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "store.json.v1.bak"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.Snapshot().Version != 2 {
		t.Fatal("identical backup blocked migration")
	}
}

// Rejecting stale edits must preserve both the current revision and persisted data.
func TestCareerStoreConflictAndIsolation(t *testing.T) {
	s, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	first := *s.Snapshot().Career
	first.Student.Skills = "Go"
	saved, err := s.SaveCareer(first, first.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveCareer(first, first.Revision); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale update accepted: %v", err)
	}
	if saved.Revision != first.Revision+1 {
		t.Fatal("revision did not advance")
	}
	saved.Student.Skills = "caller mutation"
	if s.Snapshot().Career.Student.Skills != "Go" {
		t.Fatal("returned data aliases store")
	}
	data, _ := json.Marshal(s.Snapshot().Career)
	if strings.Contains(string(data), ":null") {
		t.Fatalf("empty collections must be arrays: %s", data)
	}
}

func TestCareerStoreValidationAndFailedWrite(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	c := *s.Snapshot().Career
	c.Companies = []CareerCompany{{ID: newID(), Name: "Example", Website: "https://example.com", CareersURL: "https://boards.greenhouse.io/example?gh_jid=123", NewsURLs: []string{"https://example.com/news"}, ModelPrefixes: []string{}, Refreshes: []CareerRefresh{}}}
	c.Jobs = []CareerJob{{ID: newID(), CompanyID: c.Companies[0].ID, Provider: "greenhouse", ProviderID: "123", SourceURL: c.Companies[0].CareersURL, URL: "https://example.com/job", Title: "Graduate Engineer", Level: "new-graduate", LevelBasis: "title-inferred", DateBasis: "unknown", FetchedAt: "2026-10-03T01:00:00Z", ListingState: "listed"}}
	c.OpenRouterKey = "fixture-private-key"
	saved, err := s.SaveCareer(c, c.Revision)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*CareerState)
	}{
		{"capacity", func(c *CareerState) {
			for len(c.Companies) < 51 {
				c.Companies = append(c.Companies, CareerCompany{ID: newID(), Name: "Another"})
			}
		}},
		{"foreign-key", func(c *CareerState) { c.Jobs[0].CompanyID = newID() }},
		{"duplicate-id", func(c *CareerState) { c.Jobs = append(c.Jobs, c.Jobs[0]) }},
		{"bad-enum", func(c *CareerState) { c.Jobs[0].ListingState = "closed" }},
		{"bad-date", func(c *CareerState) { c.Jobs[0].FetchedAt = "yesterday" }},
		{"long-excerpt", func(c *CareerState) { c.Jobs[0].Text = strings.Repeat("a", 6145) }},
		{"utf8", func(c *CareerState) { c.Student.Skills = string([]byte{255}) }},
		{"key-control", func(c *CareerState) { c.OpenRouterKey = "key\nsecret" }},
		{"private-source", func(c *CareerState) { c.Companies[0].CareersURL = "https://127.0.0.1/jobs" }},
		{"credentials", func(c *CareerState) { c.Companies[0].Website = "https://secret@example.com" }},
		{"missing-usage-metadata", func(c *CareerState) {
			c.Usage.Rows = []UsageRow{{Date: "2026-10-01", Model: "example/model", TotalTokens: "0"}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			next := *s.Snapshot().Career
			tc.mutate(&next)
			if _, err := s.SaveCareer(next, next.Revision); err == nil {
				t.Fatal("invalid career state saved")
			}
			if !reflect.DeepEqual(saved, *s.Snapshot().Career) {
				t.Fatal("rejected save mutated store")
			}
		})
	}
	if err := os.WriteFile(filepath.Join(dir, "store.json.tmp"), []byte("leftover"), 0600); err != nil {
		t.Fatal(err)
	}
	next := *s.Snapshot().Career
	next.Student.Skills = "failed write"
	if _, err := s.SaveCareer(next, next.Revision); err == nil {
		t.Fatal("failed write reported saved")
	}
	if !reflect.DeepEqual(saved, *s.Snapshot().Career) {
		t.Fatal("failed write mutated store")
	}
}

func TestCareerStoreSourceEditRetainsEvidence(t *testing.T) {
	s, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	c := *s.Snapshot().Career
	id := newID()
	c.Companies = []CareerCompany{{ID: id, Name: "Example", CareersURL: "https://EXAMPLE.com:443/jobs", NewsURLs: []string{"https://example.com/news"}, Refreshes: []CareerRefresh{{Kind: "jobs", SourceURL: "https://example.com/jobs", CheckedAt: "2026-10-03T00:00:00Z", Complete: true}, {Kind: "updates", SourceURL: "https://example.com/news", CheckedAt: "2026-10-03T00:00:00Z"}}}}
	c.Updates = []CompanyUpdate{{ID: newID(), CompanyID: id, SourceURL: "https://example.com/news", URL: "https://example.com/news/a", Title: "Evidence", Kind: "article", FetchedAt: "2026-10-03T00:00:00Z"}}
	c, err = s.SaveCareer(c, c.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Companies[0].Refreshes) != 2 {
		t.Fatal("normalization lost current refresh")
	}
	c.Companies[0].CareersURL = "https://example.com/new-jobs"
	c.Companies[0].NewsURLs = nil
	c, err = s.SaveCareer(c, c.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Companies[0].Refreshes) != 0 || len(c.Updates) != 1 {
		t.Fatal("source edit discarded evidence or retained obsolete metadata")
	}
}

func TestCareerStoreStateEndpointOmitsPrivateCareer(t *testing.T) {
	s, store := testServer(t)
	c := *store.Snapshot().Career
	c.OpenRouterKey = "private-career-key"
	c.Student.Projects = "private student project"
	if _, err := store.SaveCareer(c, c.Revision); err != nil {
		t.Fatal(err)
	}
	w := apiTest(s, "GET", "/api/state", "")
	if w.Code != 200 || strings.Contains(w.Body.String(), "career") || strings.Contains(w.Body.String(), "private student") {
		t.Fatal("diagnostics state leaked private career data")
	}
}

func TestCareerStoreCapacityAndTextLimits(t *testing.T) {
	s, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, tc := range []struct {
		name   string
		mutate func(*CareerState)
	}{
		{"companies", func(c *CareerState) {
			for i := 0; i < 51; i++ {
				c.Companies = append(c.Companies, CareerCompany{ID: newID(), Name: "Company"})
			}
		}},
		{"news-urls", func(c *CareerState) {
			c.Companies[0].NewsURLs = []string{"https://example.com/1", "https://example.com/2", "https://example.com/3", "https://example.com/4"}
		}},
		{"jobs", func(c *CareerState) {
			for i := 0; i < 1001; i++ {
				c.Jobs = append(c.Jobs, CareerJob{ID: newID(), CompanyID: c.Companies[0].ID, Provider: "paste", Title: "Job", FetchedAt: "2026-10-03T00:00:00Z"})
			}
		}},
		{"updates", func(c *CareerState) {
			for i := 0; i < 301; i++ {
				c.Updates = append(c.Updates, CompanyUpdate{ID: newID(), CompanyID: c.Companies[0].ID, Kind: "pasted", Title: "News", FetchedAt: "2026-10-03T00:00:00Z"})
			}
		}},
		{"advice", func(c *CareerState) {
			for i := 0; i < 51; i++ {
				c.Advice = append(c.Advice, CareerAdvice{ID: newID(), Text: "Advice", Provider: "ollama", Model: "test", CreatedAt: "2026-10-03T00:00:00Z"})
			}
		}},
		{"company-name", func(c *CareerState) { c.Companies[0].Name = strings.Repeat("界", 161) }},
		{"student-combined", func(c *CareerState) {
			c.Student.Skills = strings.Repeat("a", 4096)
			c.Student.Projects = strings.Repeat("b", 4097)
		}},
		{"advice-text", func(c *CareerState) {
			c.Advice = []CareerAdvice{{ID: newID(), Text: strings.Repeat("a", 16385), Provider: "ollama", Model: "test", CreatedAt: "2026-10-03T00:00:00Z"}}
		}},
		{"key", func(c *CareerState) { c.OpenRouterKey = strings.Repeat("a", 513) }},
		{"url", func(c *CareerState) { c.Companies[0].Website = "https://example.com/" + strings.Repeat("a", 2048) }},
		{"source-id", func(c *CareerState) {
			c.Updates = []CompanyUpdate{{ID: newID(), CompanyID: c.Companies[0].ID, Kind: "pasted", Title: "News", SourceID: strings.Repeat("a", 513), FetchedAt: "2026-10-03T00:00:00Z"}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := s.Snapshot()
			c := *before.Career
			c.Companies = []CareerCompany{{ID: newID(), Name: "Company"}}
			tc.mutate(&c)
			if _, err := s.SaveCareer(c, c.Revision); err == nil {
				t.Fatal("capacity or text limit not enforced")
			}
			if !reflect.DeepEqual(before, s.Snapshot()) {
				t.Fatal("rejected save altered store")
			}
		})
	}
}

func TestCareerStoreLoadRejectsInvalidCareer(t *testing.T) {
	for _, kind := range []string{"null-array", "foreign-key", "bad-enum", "bad-date", "bad-utf8"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			s, err := OpenStore(dir)
			if err != nil {
				t.Fatal(err)
			}
			st := s.Snapshot()
			switch kind {
			case "null-array":
				st.Career.Jobs = nil
			case "foreign-key":
				st.Career.Updates = []CompanyUpdate{{ID: newID(), CompanyID: newID(), Kind: "pasted", Title: "News", FetchedAt: "2026-10-03T00:00:00Z"}}
			case "bad-enum":
				st.Career.Companies = []CareerCompany{{ID: newID(), Name: "Company", NewsURLs: []string{}, ModelPrefixes: []string{}, Refreshes: []CareerRefresh{}}}
				st.Career.Jobs = []CareerJob{{ID: newID(), CompanyID: st.Career.Companies[0].ID, Provider: "paste", Title: "Job", Level: "senior", LevelBasis: "unknown", DateBasis: "unknown", ListingState: "unknown", FetchedAt: "2026-10-03T00:00:00Z"}}
			case "bad-date":
				st.Career.Usage = UsageSnapshot{Rows: []UsageRow{}, AsOf: "yesterday", Version: "1", StartDate: "2026-10-01", EndDate: "2026-10-02", FetchedAt: "2026-10-03T00:00:00Z", SourceURL: "https://example.com/data"}
			}
			raw, err := json.Marshal(st)
			if err != nil {
				t.Fatal(err)
			}
			if kind == "bad-utf8" {
				raw = bytes.Replace(raw, []byte(`"skills":""`), []byte{'"', 's', 'k', 'i', 'l', 'l', 's', '"', ':', '"', 255, '"'}, 1)
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "store.json")
			if err := os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
			opened, err := OpenStore(dir)
			if err == nil {
				opened.Close()
				t.Fatal("invalid persisted career accepted")
			}
			got, _ := os.ReadFile(path)
			if !bytes.Equal(raw, got) {
				t.Fatal("invalid persisted data replaced")
			}
		})
	}
}

func TestCareerStoreURLSafetyAndNormalization(t *testing.T) {
	for _, raw := range []string{"http://example.com", "https://user:secret@example.com", "https://example.com/#secret", "https://example.com?token=secret", "https://example.com:8443/jobs", "https://localhost/jobs", "https://service.local/jobs", "https://10.0.0.1/jobs", "https://[::1]/jobs", "https://[fe80::1%25en0]/jobs", "https://example.com:/jobs"} {
		if _, err := normalizeCareerURL(raw); err == nil {
			t.Errorf("unsafe link accepted: %s", raw)
		}
	}
	got, err := normalizeCareerURL("https://BOARDS.Greenhouse.io:443/example?gh_jid=123")
	if err != nil || got != "https://boards.greenhouse.io/example?gh_jid=123" {
		t.Fatalf("recognized URL broken: %q %v", got, err)
	}
}

func TestCareerStoreCLIDefaultAdvice(t *testing.T) {
	for _, provider := range []string{"claude-cli", "codex-cli"} {
		t.Run(provider, func(t *testing.T) {
			s, err := OpenStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			c := *s.Snapshot().Career
			c.Advice = []CareerAdvice{{ID: newID(), Text: "Review the original posting requirements.", Provider: provider, CreatedAt: "2026-10-03T00:00:00Z"}}
			saved, err := s.SaveCareer(c, c.Revision)
			if err != nil {
				t.Fatal(err)
			}
			if len(saved.Advice) != 1 || saved.Advice[0].Model != "" {
				t.Fatal("CLI default model advice was not preserved")
			}
		})
	}
}
func TestCareerStoreUsageContract(t *testing.T) {
	s, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, kind := range []string{"date-only-as-of", "over-row-cap"} {
		t.Run(kind, func(t *testing.T) {
			c := *s.Snapshot().Career
			c.Usage = UsageSnapshot{Rows: []UsageRow{}, AsOf: "2026-10-03T00:00:00Z", Version: "1", StartDate: "2026-09-27", EndDate: "2026-10-03", FetchedAt: "2026-10-03T00:00:00Z", SourceURL: "https://example.com/usage"}
			if kind == "date-only-as-of" {
				c.Usage.AsOf = "2026-10-03"
			} else {
				for i := 0; i < 358; i++ {
					c.Usage.Rows = append(c.Usage.Rows, UsageRow{Date: "2026-10-01", Model: strings.Repeat("a", i+1), TotalTokens: "1"})
				}
			}
			if _, err := s.SaveCareer(c, c.Revision); err == nil {
				t.Fatal("invalid usage snapshot accepted")
			}
		})
	}
}
func TestCareerStoreSourceKindEditRetainsEvidence(t *testing.T) {
	s, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	c := *s.Snapshot().Career
	id := newID()
	c.Companies = []CareerCompany{{ID: id, Name: "Example", CareersURL: "https://example.com/source", Refreshes: []CareerRefresh{{Kind: "jobs", SourceURL: "https://example.com/source", CheckedAt: "2026-10-03T00:00:00Z"}}}}
	c.Jobs = []CareerJob{{ID: newID(), CompanyID: id, Provider: "custom", SourceURL: "https://example.com/source", Title: "Graduate Engineer", FetchedAt: "2026-10-03T00:00:00Z"}}
	c, err = s.SaveCareer(c, c.Revision)
	if err != nil {
		t.Fatal(err)
	}
	c.Companies[0].CareersURL = ""
	c.Companies[0].NewsURLs = []string{"https://example.com/source"}
	c, err = s.SaveCareer(c, c.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Companies[0].Refreshes) != 0 || len(c.Jobs) != 1 {
		t.Fatal("source kind change retained obsolete refresh or discarded evidence")
	}
}
