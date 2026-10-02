package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestResearchSourceIdentityAndBoundaries(t *testing.T) {
	for _, raw := range []string{"http://example.com/", "https://127.0.0.1/", "https://10.0.0.1/", "https://example.com:8443/", "https://user:secret@example.com/", "https://example.com/?token=secret", "https://example.com/#secret"} {
		if _, err := validateResearchURL(raw); err == nil {
			t.Fatalf("accepted unsafe source %q", raw)
		}
	}
	if _, err := validateResearchURL("https://support.example.com/help/regions"); err != nil {
		t.Fatal(err)
	}
	for _, p := range []Profile{{Name: "Claude", Origin: "https://unrelated.example"}, {Name: "Muse", Origin: "https://muse.ai"}} {
		if urls := suggestedResearchURLs(p); len(urls) != 1 || urls[0] != p.Origin {
			t.Fatalf("guessed service from name: %v", urls)
		}
	}
	urls := suggestedResearchURLs(Profile{Name: "My assistant", Origin: "https://claude.ai"})
	if len(urls) != 3 || urls[0] != "https://www.anthropic.com/supported-countries" || urls[2] != "https://status.claude.com/" {
		t.Fatalf("official domain not recognized: %v", urls)
	}
}

func TestResearchFetchExtractsPublicTextAndPreservesFailures(t *testing.T) {
	var calls int
	fixture := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			t.Error("credentials were sent")
		}
		switch r.URL.Path {
		case "/policy":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<html><head><title>Regions &amp; access</title><script>PRIVATE SCRIPT</script></head><body><nav>Navigation</nav><main><h1>Supported regions</h1><p>Available in Canada &amp; Japan.</p><p>API availability may differ.</p></main><footer>Footer</footer></body></html>`))
		case "/redirect":
			http.Redirect(w, r, "https://127.0.0.1/private", http.StatusFound)
		case "/blocked":
			w.WriteHeader(http.StatusForbidden)
		default:
			w.Header().Set("Content-Type", "application/pdf")
		}
	}))
	defer fixture.Close()
	client := fixture.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	got := fetchResearchSource(context.Background(), client, fixture.URL+"/policy")
	if got.Error != "" || !strings.Contains(got.Text, "Canada & Japan") || got.Title != "Regions & access" || got.FetchedAt == "" {
		t.Fatalf("bad source: %#v", got)
	}
	for _, hidden := range []string{"PRIVATE SCRIPT", "Navigation", "Footer"} {
		if strings.Contains(got.Text, hidden) {
			t.Fatalf("irrelevant HTML retained: %s", hidden)
		}
	}
	for _, path := range []string{"/redirect", "/blocked", "/pdf"} {
		got = fetchResearchSource(context.Background(), client, fixture.URL+path)
		if got.Error == "" || got.Text != "" {
			t.Fatalf("fetch failure presented as evidence: %#v", got)
		}
	}
	if calls != 4 {
		t.Fatalf("redirect was followed: %d calls", calls)
	}
}

func TestResearchSummaryExcludesStoredPrivateData(t *testing.T) {
	p := Profile{Name: "Example", Origin: "https://example.com", Notes: "PRIVATE NOTE", Issues: []Issue{{Error: "PRIVATE ERROR"}}}
	result := ResearchResult{ProfileID: "private-profile-id", Sources: []ResearchSource{{URL: "https://example.com/policy", Title: "Policy", Text: "Supported countries: Canada", FetchedAt: timestamp()}, {URL: "https://example.com/help", Error: "HTTP 403"}}}
	messages := researchMessages(p, &result)
	all := ""
	for _, m := range messages {
		all += m.Content
	}
	for _, private := range []string{"PRIVATE NOTE", "PRIVATE ERROR", "private-profile-id"} {
		if strings.Contains(all, private) {
			t.Fatalf("private context leaked: %s", private)
		}
	}
	if !strings.Contains(all, "Canada") || !strings.Contains(all, "HTTP 403") || !strings.Contains(all, "https://example.com/policy") {
		t.Fatal("source evidence or failure context lost")
	}
}

func TestResearchAPIRejectsStaleSelectionBeforeNetwork(t *testing.T) {
	s, store := testServer(t)
	p, err := store.SaveProfile(Profile{Name: "Example", Kind: "website", Origin: "https://example.com"})
	if err != nil {
		t.Fatal(err)
	}
	w := apiTest(s, "POST", "/api/research", `{"profileId":"`+p.ID+`","revision":0,"urls":["https://example.com"]}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("stale service status: %d %s", w.Code, w.Body.String())
	}
	w = apiTest(s, "POST", "/api/research", `{"profileId":"`+p.ID+`","revision":1,"urls":["https://127.0.0.1"]}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("private destination status: %d", w.Code)
	}
	w = apiTest(s, "POST", "/api/research", `{"profileId":"`+p.ID+`","revision":1,"urls":["https://example.com"],"settings":{"provider":"claude-cli","model":"default","cloudConfirmed":true}}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("changed model status: %d %s", w.Code, w.Body.String())
	}
}

func TestResearchDiscoveryStaysOnTheSelectedWebsite(t *testing.T) {
	links := researchLinks("https://example.com", `<a href="/pricing">Pricing</a><a href="/help/regions">Supported regions</a><a href="https://evil.example/support">Support</a><a href="/help?token=secret">Help</a><a href="/terms">Terms</a><a href="/help">Help</a>`)
	if len(links) != 2 || links[0] != "https://example.com/help/regions" || links[1] != "https://example.com/help" {
		t.Fatalf("unsafe or irrelevant discovery: %v", links)
	}
	links = researchLinks("https://support.example.com", `<nav><a href="/blog">Blog</a><a href="/contact">Contact</a></nav><footer><a href="/terms">Terms</a></footer>`)
	if len(links) != 1 || links[0] != "https://support.example.com/terms" {
		t.Fatalf("hostname distorted relevant link ranking: %v", links)
	}
}

func TestResearchOmitsNestedHeadContentAndScriptLinks(t *testing.T) {
	raw := `<head><script>hidden script</script><p>HEAD PRIVATE</p></head><body><script>const x='<a href="/regions-secret">Regions</a>';</script><p>Public policy</p><a href="/help">Help</a></body>`
	clean := researchVisibleHTML(raw)
	if strings.Contains(clean, "PRIVATE") || strings.Contains(clean, "hidden script") {
		t.Fatal("non-page text escaped extraction")
	}
	links := researchLinks("https://example.com", raw)
	if len(links) != 1 || links[0] != "https://example.com/help" {
		t.Fatalf("script link was discovered: %v", links)
	}
}

func TestResearchNeverBypassesAnExplicitProxy(t *testing.T) {
	if _, err := researchClient(Options{Proxy: "http://127.0.0.1:7890"}, []string{"https://example.com"}); err == nil {
		t.Fatal("source fetch bypassed selected proxy")
	}
}

func TestResearchBudgetsSerializedModelContext(t *testing.T) {
	for _, text := range []string{strings.Repeat("Terms & ", 768), strings.Repeat(`"`, 6<<10)} {
		result := ResearchResult{}
		for i := 0; i < 3; i++ {
			result.Sources = append(result.Sources, ResearchSource{URL: "https://example.com/policy", Text: text})
		}
		messages := researchMessages(Profile{Name: "Example", Origin: "https://example.com"}, &result)
		size := 0
		for _, m := range messages {
			size += len(m.Content)
		}
		if size > 32<<10 {
			t.Fatalf("legal excerpts exceed model budget after encoding: %d", size)
		}
		for _, source := range result.Sources {
			if len(source.Text) < len(text) && !source.Truncated {
				t.Fatal("model received a shorter extract without a visible truncation marker")
			}
		}
	}
}
