package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestCareerSourceRecognition(t *testing.T) {
	for _, tc := range []struct{ raw, provider, board, id string }{
		{"https://job-boards.greenhouse.io/example/jobs/123?gh_src=abc", "greenhouse", "example", "123"},
		{"https://boards.greenhouse.io/example", "greenhouse", "example", ""},
		{"https://jobs.ashbyhq.com/mistral.ai/post-1", "ashby", "mistral.ai", "post-1"},
		{"https://jobs.lever.co/example/id-1", "lever", "example", "id-1"},
		{"https://jobs.lever.co/example/id-1/apply", "lever", "example", "id-1"},
		{"https://jobs.ashbyhq.com/openai/id-1/application", "ashby", "openai", "id-1"},
		{"https://jobs.eu.lever.co/example", "lever-eu", "example", ""},
		{"https://example.com/careers", "custom", "", ""},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			s, e := resolveCareerSource(tc.raw, "jobs")
			if e != nil || s.Provider != tc.provider || s.Board != tc.board || s.JobID != tc.id || strings.Contains(s.URL, "?") {
				t.Fatalf("source %#v: %v", s, e)
			}
		})
	}
	for _, raw := range []string{"https://jobs.lever.co.evil.example/example?token=secret", "https://jobs.lever.co/example?unknown=1", "https://jobs.lever.co/example?limit=1", "https://user:pass@jobs.lever.co/example", "https://jobs.lever.co:8443/example", "https://127.0.0.1/jobs", "https://example.local/jobs", "https://example.com/careers?gh_jid=123", "https://jobs.ashbyhq.com/a/../b", "https://jobs.lever.co/example/id/extra", "https://jobs.lever.co/example#x"} {
		if s, e := resolveCareerSource(raw, "jobs"); e == nil {
			t.Errorf("accepted %s: %#v", raw, s)
		}
	}
}

func careerFixtureSource(t *testing.T, raw string) CareerSource {
	t.Helper()
	s, e := resolveCareerSource(raw, "jobs")
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func TestCareerJobsProviderFields(t *testing.T) {
	fixtures := []struct{ url, body, id, level, date, text string }{
		{"https://boards.greenhouse.io/example", `{"jobs":[{"id":123,"internal_job_id":999,"title":"Software Intern","location":{"name":"London"},"content":"&lt;p&gt;Student role&lt;/p&gt;","absolute_url":"https://example.com/job?gh_jid=123","first_published":"2026-01-01T00:00:00Z","updated_at":"2026-02-01T00:00:00Z"}],"meta":{"total":1}}`, "123", "internship", "published", "Student role"},
		{"https://jobs.ashbyhq.com/example", `{"jobs":[{"id":"a","title":"Research Engineer","jobUrl":"https://jobs.ashbyhq.com/example/a","isListed":true,"publishedAt":"2026-01-01T00:00:00Z","descriptionPlain":"Build systems","workplaceType":"Remote","employmentType":"FullTime"},{"id":"b","title":"Hidden","isListed":false}]}`, "a", "unknown", "last-published", "Build systems"},
		{"https://jobs.eu.lever.co/example", `[{"id":"l","text":"Graduate Engineer","hostedUrl":"https://jobs.eu.lever.co/example/l","categories":{"location":"Paris","commitment":"FullTime"},"descriptionPlain":"Build","lists":[{"text":"Requirements","content":"<li>New graduates welcome</li>"}],"additionalPlain":"Closing text","workplaceType":"hybrid"}]`, "l", "new-graduate", "unknown", "Closing text"},
	}
	for _, f := range fixtures {
		b, e := parseCareerJobs(careerFixtureSource(t, f.url), []byte(f.body))
		if e != nil || len(b.Jobs) != 1 || !b.Complete {
			t.Fatalf("%s: %#v %v", f.url, b, e)
		}
		j := b.Jobs[0]
		if j.ProviderID != f.id || j.Level != f.level || j.DateBasis != f.date || !strings.Contains(j.Text, f.text) {
			t.Fatalf("wrong fields %#v", j)
		}
	}
	for _, title := range []string{"Internal Systems Engineer", "Research Engineer", "Internals Engineer", "Senior Engineer"} {
		b, e := parseCareerJobs(CareerSource{Provider: "paste", Kind: "jobs"}, []byte(title+"\nBuild great systems"))
		if e != nil || b.Jobs[0].Level != "unknown" {
			t.Fatalf("false signal %s: %#v %v", title, b, e)
		}
	}
	b, e := parseCareerJobs(CareerSource{Provider: "paste", Kind: "jobs"}, []byte("Engineer\nThis is an entry-level position."))
	if e != nil || b.Jobs[0].Level != "entry-level" || b.Jobs[0].LevelBasis != "posting-explicit" {
		t.Fatalf("explicit level: %#v %v", b, e)
	}
}
func TestCareerJobsMalformedAndCaps(t *testing.T) {
	s := careerFixtureSource(t, "https://boards.greenhouse.io/example")
	for _, raw := range []string{`{}`, `{"jobs":null}`, `{"jobs":[{}]}`, `{"jobs":[{"id":1,"title":"a"},{"id":1,"title":"b"}]}`, `{"jobs":[],"meta":{"total":2}}`} {
		b, e := parseCareerJobs(s, []byte(raw))
		if e == nil && b.Complete {
			t.Errorf("trusted malformed %s", raw)
		}
	}
	b, e := parseCareerJobs(s, []byte(`{"jobs":[]}`))
	if e != nil || !b.Complete || b.Jobs == nil {
		t.Fatalf("empty array: %#v %v", b, e)
	}
	records := []map[string]any{}
	for i := 0; i < 501; i++ {
		records = append(records, map[string]any{"id": fmt.Sprint(i), "title": "Engineer", "isListed": true, "jobUrl": fmt.Sprintf("https://jobs.ashbyhq.com/example/%d", i)})
	}
	raw, _ := json.Marshal(map[string]any{"jobs": records})
	a := careerFixtureSource(t, "https://jobs.ashbyhq.com/example")
	b, e = parseCareerJobs(a, raw)
	if e != nil || len(b.Jobs) != 500 || b.Complete {
		t.Fatalf("cap: %d %v %v", len(b.Jobs), b.Complete, e)
	}
	a.JobID = "500"
	a.URL += "/500"
	b, e = parseCareerJobs(a, raw)
	if e != nil || len(b.Jobs) != 1 || b.Jobs[0].ProviderID != "500" || b.Scope != "posting" {
		t.Fatalf("single beyond cap: %#v %v", b, e)
	}
}
func TestCareerJobsCustomJSONLD(t *testing.T) {
	s := careerFixtureSource(t, "https://example.com/careers")
	raw := `<title>Careers</title><script type="application/ld+json">{"@context":"https://schema.org","@graph":[{"@type":"JobPosting","identifier":{"value":"1"},"title":"New Graduate Engineer","description":"<p>Build software</p>","url":"/jobs/1","datePosted":"2026-01-01","jobLocation":{"address":{"addressLocality":"London","addressCountry":"UK"}},"jobLocationType":"TELECOMMUTE"}]}</script>`
	b, e := parseCareerJobs(s, []byte(raw))
	if e != nil || len(b.Jobs) != 1 || b.Jobs[0].URL != "https://example.com/jobs/1" || b.Jobs[0].PublishedAt != "2026-01-01" || b.Complete {
		t.Fatalf("custom %#v %v", b, e)
	}
	b, e = parseCareerJobs(s, []byte(`<title>Careers</title><main>Engineering opportunities</main><script>secret()</script>`))
	if e != nil || len(b.Jobs) != 1 || strings.Contains(b.Jobs[0].Text, "secret") {
		t.Fatalf("fallback %#v %v", b, e)
	}
}

func TestCareerJobsCustomIdentifiersPreserveRefreshIdentity(t *testing.T) {
	source := careerFixtureSource(t, "https://example.com/careers")
	raw := `<script type="application/ld+json">{"@graph":[{"@type":"JobPosting","identifier":"intern-1","title":"Software Intern","description":"Build services"},{"@type":"JobPosting","identifier":{"@type":"PropertyValue","value":"graduate-2"},"title":"Graduate Engineer","description":"Build software"}]}</script>`
	batch, err := parseCareerJobs(source, []byte(raw))
	if err != nil || len(batch.Jobs) != 2 {
		t.Fatalf("parse %#v: %v", batch, err)
	}
	current := careerMergeState()
	current, err = mergeCareerJobs(current, current.Companies[0].ID, batch)
	if err != nil || len(current.Jobs) != 2 {
		t.Fatalf("distinct identifiers could not merge: %#v %v", current.Jobs, err)
	}
	ids := map[string]string{}
	for _, j := range current.Jobs {
		ids[j.ProviderID] = j.ID
		if j.URL != "" || j.ListingState != "unknown" {
			t.Fatalf("unverified URL/listing %#v", j)
		}
	}
	if ids["intern-1"] == "" || ids["graduate-2"] == "" {
		t.Fatalf("explicit identifiers lost: %#v", ids)
	}
	raw = strings.ReplaceAll(raw, "Build services", "Updated services")
	batch, err = parseCareerJobs(source, []byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	batch.Jobs[0], batch.Jobs[1] = batch.Jobs[1], batch.Jobs[0]
	next, err := mergeCareerJobs(current, current.Companies[0].ID, batch)
	if err != nil || len(next.Jobs) != 2 {
		t.Fatalf("refresh duplicated postings: %#v %v", next.Jobs, err)
	}
	for _, j := range next.Jobs {
		if j.ID != ids[j.ProviderID] || j.ListingState != "unknown" || j.ProviderID == "intern-1" && j.Text != "Updated services" {
			t.Fatalf("refresh lost identity/evidence %#v", j)
		}
	}
}

func TestCareerJobsCustomIdentifierBoundaries(t *testing.T) {
	source := careerFixtureSource(t, "https://example.com/careers")
	for _, identifier := range []any{nil, []string{"a", "b"}, map[string]any{"value": 123}, strings.Repeat("x", 513), "a\nb", "https://example.com/job?token=secret", "https://127.0.0.1/job"} {
		posting := map[string]any{"@type": "JobPosting", "title": "Engineer", "identifier": identifier, "url": "https://example.com/job?token=secret"}
		body, _ := json.Marshal(posting)
		batch, err := parseCareerJobs(source, []byte(`<script type="application/ld+json">`+string(body)+`</script>`))
		if err != nil || len(batch.Jobs) != 1 || batch.Jobs[0].URL != "" || batch.Jobs[0].ProviderID != source.URL {
			t.Fatalf("unsafe/unsupported identity retained %#v: %v", batch.Jobs, err)
		}
		batch.Jobs = append(batch.Jobs, batch.Jobs[0])
		current := careerMergeState()
		if _, err := mergeCareerJobs(current, current.Companies[0].ID, batch); err == nil {
			t.Fatal("ambiguous identities accepted")
		}
	}
}

type careerRoundTrip func(*http.Request) (*http.Response, error)

func (f careerRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func careerResponse(body string) *http.Response {
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
}
func TestCareerJobsFetchPagination(t *testing.T) {
	source := careerFixtureSource(t, "https://jobs.eu.lever.co/example")
	calls := 0
	client := &http.Client{Transport: careerRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Host != "api.eu.lever.co" || r.URL.Query().Get("skip") != fmt.Sprint((calls-1)*100) || r.URL.Query().Get("limit") != "100" || r.Header.Get("Accept") != "application/json" {
			t.Fatalf("wrong request %s %#v", r.URL, r.Header)
		}
		if _, ok := r.Context().Deadline(); !ok {
			t.Fatal("no deadline")
		}
		jobs := []map[string]any{}
		if calls == 1 {
			for i := 0; i < 100; i++ {
				jobs = append(jobs, map[string]any{"id": fmt.Sprint(i), "text": "Engineer"})
			}
		}
		raw, _ := json.Marshal(jobs)
		return careerResponse(string(raw)), nil
	})}
	b, e := fetchCareerJobs(context.Background(), client, source)
	if e != nil || calls != 2 || len(b.Jobs) != 100 || !b.Complete {
		t.Fatalf("pagination %#v %v calls=%d", b, e, calls)
	}
	calls = 0
	client.Transport = careerRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		jobs := []map[string]any{}
		for i := 0; i < 100; i++ {
			jobs = append(jobs, map[string]any{"id": fmt.Sprint(i), "text": "Engineer"})
		}
		raw, _ := json.Marshal(jobs)
		return careerResponse(string(raw)), nil
	})
	b, e = fetchCareerJobs(context.Background(), client, source)
	if e == nil && b.Complete {
		t.Fatal("repeated page marked complete")
	}
}
func TestCareerJobsFetchPolicyAndBounds(t *testing.T) {
	s := careerFixtureSource(t, "https://jobs.ashbyhq.com/mistral.ai/500")
	client := &http.Client{Transport: careerRoundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "https://api.ashbyhq.com/posting-api/job-board/mistral.ai" {
			t.Fatalf("invented endpoint %s", r.URL)
		}
		return careerResponse(`{"jobs":[{"id":"500","title":"Engineer","isListed":true,"descriptionPlain":"` + strings.Repeat("x", 2<<20) + `"}]}`), nil
	})}
	b, e := fetchCareerJobs(context.Background(), client, s)
	if e != nil || len(b.Jobs) != 1 || !b.Jobs[0].Truncated {
		t.Fatalf("known >2 MiB: %#v %v", b, e)
	}
	client.Transport = careerRoundTrip(func(r *http.Request) (*http.Response, error) {
		return careerResponse(strings.Repeat("x", (16<<20)+1)), nil
	})
	if _, e = fetchCareerJobs(context.Background(), client, s); e == nil {
		t.Fatal("oversized ATS accepted")
	}
	s.Provider = "lever"
	if _, e = fetchCareerJobs(context.Background(), client, s); e == nil {
		t.Fatal("forged source accepted")
	}
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1234")
	t.Setenv("NO_PROXY", "")
	if _, e = fetchCareerJobs(context.Background(), nil, careerFixtureSource(t, "https://example.com/jobs")); e == nil {
		t.Fatal("custom proxy fallback")
	}
}
func careerMergeState() CareerState {
	c := *newCareerState()
	c.Companies = []CareerCompany{{ID: newID(), Name: "One", CareersURL: "https://boards.greenhouse.io/example", NewsURLs: []string{}, ModelPrefixes: []string{}, Refreshes: []CareerRefresh{}}, {ID: newID(), Name: "Two", NewsURLs: []string{}, ModelPrefixes: []string{}, Refreshes: []CareerRefresh{}}}
	return c
}
func TestCareerMergeSnapshotIdentity(t *testing.T) {
	c := careerMergeState()
	s := careerFixtureSource(t, "https://boards.greenhouse.io/example")
	b, e := parseCareerJobs(s, []byte(`{"jobs":[{"id":1,"title":"Engineer"},{"id":2,"title":"Intern"}]}`))
	if e != nil {
		t.Fatal(e)
	}
	c, e = mergeCareerJobs(c, c.Companies[0].ID, b)
	if e != nil {
		t.Fatal(e)
	}
	first := c.Jobs[0].ID
	c, e = mergeCareerJobs(c, c.Companies[1].ID, b)
	if e != nil || len(c.Jobs) != 4 {
		t.Fatalf("cross-company %#v %v", c, e)
	}
	b.Jobs = b.Jobs[:1]
	b.Jobs[0].URL = "https://example.com/changed"
	before := append([]CareerJob{}, c.Jobs...)
	next, e := mergeCareerJobs(c, c.Companies[0].ID, b)
	if e != nil || next.Jobs[0].ID != first || next.Jobs[1].ListingState != "no-longer-listed" || next.Jobs[3].ListingState != "listed" || !reflect.DeepEqual(c.Jobs, before) {
		t.Fatalf("merge %#v %v", next.Jobs, e)
	}
	for _, mode := range []string{"posting", "incomplete", "changed-board"} {
		t.Run(mode, func(t *testing.T) {
			bb := b
			bb.Jobs = []CareerJob{}
			switch mode {
			case "posting":
				bb.Scope = "posting"
			case "incomplete":
				bb.Complete = false
			case "changed-board":
				bb.Source.Board = "other"
				bb.Source.URL = "https://boards.greenhouse.io/other"
			}
			n, e := mergeCareerJobs(c, c.Companies[0].ID, bb)
			if e != nil || n.Jobs[1].ListingState != "listed" {
				t.Fatalf("closed unrelated snapshot %#v %v", n, e)
			}
		})
	}
}
func TestCareerMergeCapacityAtomic(t *testing.T) {
	c := careerMergeState()
	for i := 0; i < 1000; i++ {
		c.Jobs = append(c.Jobs, CareerJob{ID: newID(), CompanyID: c.Companies[0].ID, Provider: "paste", Title: "Job", FetchedAt: timestamp(), Level: "unknown", LevelBasis: "unknown", DateBasis: "unknown", ListingState: "unknown"})
	}
	b, e := parseCareerJobs(careerFixtureSource(t, "https://boards.greenhouse.io/example"), []byte(`{"jobs":[{"id":1,"title":"Engineer"}]}`))
	if e != nil {
		t.Fatal(e)
	}
	before := append([]CareerJob{}, c.Jobs...)
	if _, e = mergeCareerJobs(c, c.Companies[0].ID, b); e == nil || !reflect.DeepEqual(c.Jobs, before) {
		t.Fatalf("capacity failure mutated input %v", e)
	}
}

func TestCareerJobsBoundaryGuards(t *testing.T) {
	source := careerFixtureSource(t, "https://boards.greenhouse.io/example")
	if _, err := parseCareerJobs(source, []byte(`{"jobs":[{"id":"abc","title":"Engineer"}]}`)); err == nil {
		t.Fatal("nonnumeric Greenhouse identity accepted")
	}
	if _, err := parseCareerJobs(source, []byte{0xff}); err == nil {
		t.Fatal("non-UTF-8 accepted")
	}
	jobs := []map[string]any{}
	for i := 1; i <= 500; i++ {
		jobs = append(jobs, map[string]any{"id": i, "title": "Engineer"})
	}
	body, _ := json.Marshal(map[string]any{"jobs": jobs})
	batch, err := parseCareerJobs(source, body)
	if err != nil || batch.Complete || len(batch.Jobs) != 500 {
		t.Fatalf("exact cap falsely complete %#v %v", batch, err)
	}
	custom := careerFixtureSource(t, "https://example.com/jobs")
	raw := `{"@type":"JobPosting","title":"Engineer"}`
	for i := 0; i < 10; i++ {
		raw = `{"nested":` + raw + `}`
	}
	if _, err := parseCareerJobs(custom, []byte(`<script type="application/ld+json">`+raw+`</script>`)); err == nil {
		t.Fatal("deep JSON-LD accepted")
	}
	raw = `[` + strings.Repeat(`{},`, 1001) + `{}]`
	if _, err := parseCareerJobs(custom, []byte(`<script type="application/ld+json">`+raw+`</script>`)); err == nil {
		t.Fatal("unbounded JSON-LD accepted")
	}
	if _, err := parseCareerJobs(custom, []byte(strings.Repeat("x", (2<<20)+1))); err == nil {
		t.Fatal("oversized custom page accepted")
	}
}
func TestCareerMergeRejectsForgedBatch(t *testing.T) {
	c := careerMergeState()
	b, err := parseCareerJobs(careerFixtureSource(t, "https://boards.greenhouse.io/example"), []byte(`{"jobs":[{"id":1,"title":"Engineer"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	b.Complete = false
	b.Source.Board = "other"
	if _, err := mergeCareerJobs(c, c.Companies[0].ID, b); err == nil {
		t.Fatal("forged incomplete source accepted")
	}
}

func TestCareerJobsSourceClientPreservesRoute(t *testing.T) {
	custom := careerFixtureSource(t, "https://example.com/jobs")
	if _, err := careerSourceClient(Options{Proxy: "socks5://127.0.0.1:1234"}, custom); err == nil {
		t.Fatal("custom SOCKS proxy accepted")
	}
	fixed := careerFixtureSource(t, "https://jobs.ashbyhq.com/example")
	if client, err := careerSourceClient(Options{Proxy: "socks5://127.0.0.1:1234"}, fixed); err != nil {
		t.Fatal(err)
	} else {
		client.CloseIdleConnections()
	}
	calls := 0
	originalRedirect := func(*http.Request, []*http.Request) error { return nil }
	client := &http.Client{CheckRedirect: originalRedirect, Transport: careerRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.String() != custom.URL {
			t.Fatalf("route changed %s", r.URL)
		}
		res := careerResponse(`<title>Jobs</title><main>Engineer</main>`)
		res.Header.Set("Content-Type", "text/html; charset=utf-8")
		return res, nil
	})}
	batch, err := fetchCareerJobs(context.Background(), client, custom)
	if err != nil || calls != 1 || len(batch.Jobs) != 1 {
		t.Fatalf("injected route replaced %#v %v calls=%d", batch, err, calls)
	}
	if client.CheckRedirect == nil || reflect.ValueOf(client.CheckRedirect).Pointer() != reflect.ValueOf(originalRedirect).Pointer() {
		t.Fatal("caller client mutated")
	}
	if _, err := fetchCareerJobs(context.Background(), nil, fixed); err == nil {
		t.Fatal("nil client silently created route")
	}
}

func TestCareerJobsRejectsUnverifiedShellAndListing(t *testing.T) {
	ashby := careerFixtureSource(t, "https://jobs.ashbyhq.com/example")
	batch, err := parseCareerJobs(ashby, []byte(`{"jobs":[{"id":"1","title":"Engineer"}]}`))
	if err == nil && batch.Complete && batch.Jobs[0].ListingState == "listed" {
		t.Fatal("missing Ashby listing flag confirmed")
	}
	custom := careerFixtureSource(t, "https://example.com/jobs")
	for _, raw := range []string{`<title>Jobs</title><main>Please enable JavaScript to view this page.</main>`, `<title>Just a moment...</title><main>Verify you are human</main>`} {
		if _, err := parseCareerJobs(custom, []byte(raw)); err == nil {
			t.Fatalf("shell accepted %s", raw)
		}
	}
}
func TestCareerJobsJSONLDNestedArrays(t *testing.T) {
	custom := careerFixtureSource(t, "https://example.com/jobs")
	for _, raw := range []string{`{"@type":[["JobPosting"]],"title":"Engineer"}`, `{"@type":"JobPosting","title":"Engineer","jobLocation":[[{"address":{"addressLocality":"London"}}]]}`} {
		if _, err := parseCareerJobs(custom, []byte(`<script type="application/ld+json">`+raw+`</script>`)); err == nil {
			t.Fatalf("nested schema arrays accepted %s", raw)
		}
	}
}

func TestCareerJobsCustomUnsafeLinkNotIdentity(t *testing.T) {
	source := careerFixtureSource(t, "https://example.com/jobs")
	batch, err := parseCareerJobs(source, []byte(`<script type="application/ld+json">{"@type":"JobPosting","title":"Engineer","url":"https://example.com/job?token=secret"}</script>`))
	if err != nil {
		t.Fatal(err)
	}
	if batch.Jobs[0].URL != "" || strings.Contains(batch.Jobs[0].ProviderID, "secret") {
		t.Fatalf("unsafe link retained %#v", batch.Jobs[0])
	}
}

func TestCareerJobsFetchFivePageCap(t *testing.T) {
	source := careerFixtureSource(t, "https://jobs.lever.co/example")
	calls := 0
	client := &http.Client{Transport: careerRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		jobs := []map[string]any{}
		for i := 0; i < 100; i++ {
			jobs = append(jobs, map[string]any{"id": fmt.Sprintf("%d-%d", calls, i), "text": "Engineer"})
		}
		body, _ := json.Marshal(jobs)
		return careerResponse(string(body)), nil
	})}
	batch, err := fetchCareerJobs(context.Background(), client, source)
	if err != nil || calls != 5 || len(batch.Jobs) != 500 || batch.Complete {
		t.Fatalf("pagination cap calls=%d jobs=%d complete=%v error=%v", calls, len(batch.Jobs), batch.Complete, err)
	}
}

func TestCareerMergeRetiresOnlyPreviouslyListed(t *testing.T) {
	c := careerMergeState()
	b, err := parseCareerJobs(careerFixtureSource(t, "https://boards.greenhouse.io/example"), []byte(`{"jobs":[{"id":1,"title":"Engineer"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	c, err = mergeCareerJobs(c, c.Companies[0].ID, b)
	if err != nil {
		t.Fatal(err)
	}
	c.Jobs[0].ListingState = "unknown"
	b.Jobs = []CareerJob{}
	next, err := mergeCareerJobs(c, c.Companies[0].ID, b)
	if err != nil || next.Jobs[0].ListingState != "unknown" {
		t.Fatalf("unverified listing retired %#v %v", next.Jobs, err)
	}
}

func TestCareerSourceClientReadBudgetKeepsRouteBoundaries(t *testing.T) {
	fixed := careerFixtureSource(t, "https://jobs.ashbyhq.com/example")
	client, err := careerSourceClient(Options{Proxy: "http://127.0.0.1:1234"}, fixed)
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	if client.Timeout != 30*time.Second {
		t.Fatalf("fixed ATS overall read budget = %v; want 30s", client.Timeout)
	}
	tr := client.Transport.(*http.Transport)
	baseline, err := networkClient(Options{Proxy: "http://127.0.0.1:1234"})
	if err != nil {
		t.Fatal(err)
	}
	defer baseline.CloseIdleConnections()
	baseTransport := baseline.Transport.(*http.Transport)
	if tr.ResponseHeaderTimeout != 8*time.Second || tr.TLSHandshakeTimeout != baseTransport.TLSHandshakeTimeout || !reflect.DeepEqual(tr.TLSClientConfig, baseTransport.TLSClientConfig) {
		t.Fatal("changed header/TLS boundaries")
	}
	req, _ := http.NewRequest(http.MethodGet, "https://api.ashbyhq.com/", nil)
	proxy, err := tr.Proxy(req)
	if err != nil || proxy == nil || proxy.String() != "http://127.0.0.1:1234" {
		t.Fatal("fixed ATS route was replaced")
	}
	custom := careerFixtureSource(t, "https://example.com/careers")
	client, err = careerSourceClient(Options{}, custom)
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	if client.Timeout != 8*time.Second || client.Transport.(*http.Transport).DialContext == nil {
		t.Fatal("custom timeout or public DNS enforcement changed")
	}
}

type careerReadError struct{ err error }

func (r careerReadError) Read([]byte) (int, error) { return 0, r.err }

func TestCareerRecruitingBodyFailuresAreActionableAndSanitized(t *testing.T) {
	for _, tc := range []struct {
		name string
		body io.Reader
		want string
	}{
		{"canceled", careerReadError{context.Canceled}, "read was canceled"},
		{"timeout", careerReadError{context.DeadlineExceeded}, "read timed out"},
		{"unreadable", careerReadError{fmt.Errorf("secret transport https://user:password@example.com")}, "could not be read"},
		{"oversized", strings.NewReader("123456"), "exceeded its size limit"},
		{"utf8", strings.NewReader(string([]byte{0xff})), "not valid UTF-8"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &http.Client{Transport: careerRoundTrip(func(*http.Request) (*http.Response, error) {
				res := careerResponse("")
				res.Body = io.NopCloser(tc.body)
				return res, nil
			})}
			_, err := readCareerResponse(context.Background(), client, "https://api.ashbyhq.com/", true, 5)
			if err == nil || !strings.Contains(err.Error(), tc.want) || strings.Contains(err.Error(), "password") || strings.Contains(err.Error(), "https://") || !strings.Contains(err.Error(), "paste") {
				t.Fatalf("missing actionable sanitized %s error: %v", tc.name, err)
			}
		})
	}
}
