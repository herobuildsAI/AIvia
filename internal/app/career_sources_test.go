package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func careerUpdateSource(t *testing.T) CareerSource {
	t.Helper()
	s, err := resolveCareerSource("https://news.example.com/feed", "updates")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestCareerUpdatesFeeds(t *testing.T) {
	source := careerUpdateSource(t)
	for _, tt := range []struct{ name, media, body string }{
		{"rss", "application/rss+xml; charset=utf-8", `<rss version="2.0"><channel><item><guid>release-1</guid><title>New &amp; useful</title><link>/release</link><description><![CDATA[<p>Released <b>today</b>.</p>]]></description><pubDate>Thu, 01 Oct 2026 09:00:00 +0000</pubDate></item></channel></rss>`},
		{"atom", "application/atom+xml", `<feed xmlns="http://www.w3.org/2005/Atom"><entry><id>release-1</id><title>New &amp; useful</title><link rel="self" href="/api"/><link rel="alternate" href="/release"/><content type="html">&lt;p&gt;Released &lt;b&gt;today&lt;/b&gt;.&lt;/p&gt;</content><published>2026-10-01T09:00:00Z</published><updated>2026-10-02T10:00:00Z</updated></entry></feed>`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseCareerUpdates(source, tt.media, []byte(tt.body))
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 1 || got[0].Kind != "feed-item" || got[0].SourceID != "release-1" || got[0].URL != "https://news.example.com/release" || got[0].PublishedAt != "2026-10-01T09:00:00Z" || got[0].SourceURL != source.URL || !strings.Contains(got[0].Text, "Released") || strings.Contains(got[0].Text, "<") || got[0].FetchedAt == "" {
				t.Fatalf("unexpected feed: %+v", got)
			}
		})
	}
}

func TestCareerUpdatesUnknownDatesUnsafeLinksAndDuplicates(t *testing.T) {
	body := `<rss><channel><item><guid>same</guid><title>First</title><link>https://127.0.0.1/a</link></item><item><guid>same</guid><title>Duplicate</title></item><item><guid>two</guid><title>Safe</title><link>/safe?b=2&amp;a=1</link></item><item><guid>three</guid><title>Duplicate URL</title><link>https://news.example.com/safe?a=1&amp;b=2</link></item><item><guid>four</guid><title>Unsafe</title><link>javascript:alert(1)</link></item></channel></rss>`
	got, err := parseCareerUpdates(careerUpdateSource(t), "application/xml", []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].URL != "" || got[0].PublishedAt != "" || got[2].URL != "" || got[2].SourceURL == "" {
		t.Fatalf("unsafe/duplicate feed: %+v", got)
	}
	atom := `<feed xmlns="http://www.w3.org/2005/Atom"><entry><id>x</id><title>Undated</title><updated>2026-10-01T12:00:00Z</updated></entry></feed>`
	got, err = parseCareerUpdates(careerUpdateSource(t), "application/atom+xml", []byte(atom))
	if err != nil || len(got) != 1 || got[0].PublishedAt != "" {
		t.Fatalf("updated date invented publication: %+v %v", got, err)
	}
}

func TestCareerUpdatesHTMLSnapshot(t *testing.T) {
	raw := `<html><head><title>Company news</title></head><body><nav>Menu</nav><main><h1>Company news</h1><p>Research details.</p><a href="http://localhost/">Read</a></main><script>secret()</script></body></html>`
	got, err := parseCareerUpdates(careerUpdateSource(t), "text/html", []byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Kind != "page-snapshot" || got[0].PublishedAt != "" || got[0].URL != got[0].SourceURL || got[0].Title != "Company news" || strings.Contains(got[0].Text, "secret") || strings.Contains(got[0].Text, "localhost") || strings.Contains(got[0].Text, "Menu") {
		t.Fatalf("snapshot: %+v", got)
	}
}

func TestCareerUpdatesRejectsBrowserShell(t *testing.T) {
	for _, raw := range []string{`<html><title>Just a moment</title><body>Checking your browser</body></html>`, `<html><body>Enable JavaScript to read company news</body></html>`} {
		if _, err := parseCareerUpdates(careerUpdateSource(t), "text/html", []byte(raw)); err == nil {
			t.Fatal("accepted browser shell")
		}
	}
}

func TestCareerUpdatesRejectInvalidAndBoundInput(t *testing.T) {
	for _, body := range []string{
		`<rss><channel><item><title>x</title><pubDate>2026-02-30</pubDate></item></channel></rss>`,
		`<!DOCTYPE rss [<!ENTITY x SYSTEM "https://example.com/evil">]><rss><channel/></rss>`,
		`<!bad directive><rss><channel/></rss>`,
		`<rss><channel><item><title>unfinished`, `<unexpected/>`,
		strings.Repeat(`<a>`, 33) + strings.Repeat(`</a>`, 33),
		`<rss><channel><item><title>x</title><guid>` + strings.Repeat("a", 513) + `</guid></item></channel></rss>`,
		`<rss><channel>` + strings.Repeat(`<item><title>x</title></item>`, 5001) + `</channel></rss>`,
	} {
		if _, err := parseCareerUpdates(careerUpdateSource(t), "application/xml", []byte(body)); err == nil {
			t.Fatalf("accepted invalid input of %d bytes", len(body))
		}
	}
	for _, media := range []string{"application/json", "image/png", "text/html; charset=iso-8859-1"} {
		if _, err := parseCareerUpdates(careerUpdateSource(t), media, []byte(`{}`)); err == nil {
			t.Fatalf("accepted %s", media)
		}
	}
	if _, err := parseCareerUpdates(careerUpdateSource(t), "text/html", []byte(strings.Repeat("a", (2<<20)+1))); err == nil {
		t.Fatal("accepted oversized page")
	}
	if _, err := parseCareerUpdates(careerUpdateSource(t), "text/html", []byte{0xff}); err == nil {
		t.Fatal("accepted invalid UTF-8")
	}
	body := `<rss><channel>`
	for i := 0; i < 25; i++ {
		body += fmt.Sprintf(`<item><guid>%d</guid><title>Item %d</title><description>%s</description></item>`, i, i, strings.Repeat("x", 7000))
	}
	body += `</channel></rss>`
	got, err := parseCareerUpdates(careerUpdateSource(t), "application/xml", []byte(body))
	if err != nil || len(got) != 20 || !got[0].Truncated || len(got[0].Text) > 6144 {
		t.Fatalf("cap: %d %v", len(got), err)
	}
}

func TestCareerUpdatesLargeFeedRetainsOutputCap(t *testing.T) {
	var body strings.Builder
	body.WriteString(`<rss><channel>`)
	for i := 0; i < 1245; i++ {
		fmt.Fprintf(&body, `<item><guid>%d</guid><title>Item %d</title><description>Public update</description></item>`, i, i)
	}
	body.WriteString(`</channel></rss>`)
	got, err := parseCareerUpdates(careerUpdateSource(t), "application/xml", []byte(body.String()))
	if err != nil || len(got) != 20 || got[0].SourceID != "0" || got[19].SourceID != "19" {
		t.Fatalf("large feed must retain bounded first 20 items: %d %v", len(got), err)
	}
}

func careerDataset(start, end string, rows any) string {
	body, _ := json.Marshal(map[string]any{"data": rows, "meta": map[string]string{"as_of": "2026-10-03T00:00:00Z", "version": "v1", "start_date": start, "end_date": end}})
	return string(body)
}

func TestCareerUsagePrecisionAndMetadata(t *testing.T) {
	raw := []byte(`{"data":[{"date":"2026-10-01","model_permaslug":"example/model","total_tokens":"9007199254740993"}],"meta":{"as_of":"2026-10-02T00:00:00Z","version":"v1","start_date":"2026-10-01","end_date":"2026-10-01"}}`)
	got, err := parseCareerUsage(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Rows) != 1 || got.Rows[0].TotalTokens != "9007199254740993" || got.Rows[0].Model != "example/model" || got.AsOf != "2026-10-02T00:00:00Z" || got.Version != "v1" || got.StartDate != "2026-10-01" || got.EndDate != "2026-10-01" || got.SourceURL != "https://openrouter.ai/rankings" || got.FetchedAt == "" {
		t.Fatalf("precision/meta: %+v", got)
	}
	if err := validateCareerUsage(got); err != nil {
		t.Fatal(err)
	}
}

func TestCareerUsageRejectInvalidDataset(t *testing.T) {
	valid := careerDataset("2026-10-01", "2026-10-02", []map[string]any{{"date": "2026-10-01", "model_permaslug": "example/model", "total_tokens": "0"}})
	for _, body := range []string{
		`{}`, `{"data":null,"meta":{}}`, valid + `{}`, strings.Replace(valid, `"0"`, `9007199254740993`, 1),
		strings.Replace(valid, `"0"`, `"-1"`, 1), strings.Replace(valid, `"0"`, `"1.5"`, 1), strings.Replace(valid, `"0"`, `"1e3"`, 1),
		strings.Replace(valid, `"0"`, `"01"`, 1), strings.Replace(valid, `"0"`, `"`+strings.Repeat("9", 101)+`"`, 1),
		strings.Replace(valid, `"example/model"`, `""`, 1), strings.Replace(valid, `"2026-10-01"`, `"2026-02-30"`, 1),
		careerDataset("2026-10-02", "2026-10-01", []any{}), careerDataset("2026-09-01", "2026-10-01", []any{}),
		careerDataset("2026-10-01", "2026-10-02", []map[string]string{{"date": "2026-10-03", "model_permaslug": "x", "total_tokens": "1"}}),
		careerDataset("2026-10-01", "2026-10-02", []map[string]string{{"date": "2026-10-01", "model_permaslug": "x", "total_tokens": "1"}, {"date": "2026-10-01", "model_permaslug": "x", "total_tokens": "2"}}),
	} {
		if _, err := parseCareerUsage([]byte(body)); err == nil {
			t.Fatalf("accepted invalid dataset: %.150s", body)
		}
	}
	rows := []map[string]string{}
	for i := 0; i < 52; i++ {
		rows = append(rows, map[string]string{"date": "2026-10-01", "model_permaslug": fmt.Sprintf("model/%d", i), "total_tokens": "1"})
	}
	if _, err := parseCareerUsage([]byte(careerDataset("2026-10-01", "2026-10-02", rows))); err == nil {
		t.Fatal("accepted >51 rows in one day")
	}
	if _, err := parseCareerUsage([]byte(strings.Repeat(" ", (2<<20)+1))); err == nil {
		t.Fatal("accepted oversized dataset")
	}
	empty, err := parseCareerUsage([]byte(careerDataset("2026-10-01", "2026-10-02", []any{})))
	if err != nil || empty.Rows == nil || len(empty.Rows) != 0 {
		t.Fatalf("empty rows: %+v %v", empty, err)
	}
}

func TestCareerUsageFixedRequestAndUpdatesUseInjectedRoute(t *testing.T) {
	now := time.Date(2026, 10, 3, 3, 0, 0, 0, time.FixedZone("east", 8*3600)) // UTC is October 2.
	calls := 0
	client := &http.Client{Transport: careerRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Host != "openrouter.ai" || r.URL.Path != "/api/v1/datasets/rankings-daily" || r.Header.Get("Authorization") != "Bearer synthetic-key" || r.URL.Query().Get("start_date") != "2026-09-25" || r.URL.Query().Get("end_date") != "2026-10-01" || len(r.URL.Query()) != 2 || strings.Contains(r.URL.String(), "synthetic-key") {
			t.Fatalf("bad fixed request: %s", r.URL)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(careerDataset("2026-09-25", "2026-10-01", []any{})))}, nil
	})}
	if _, err := fetchCareerUsage(context.Background(), client, "synthetic-key", now); err != nil {
		t.Fatal(err)
	}
	client.Transport = careerRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Header.Get("Authorization") != "" || r.URL.String() != careerUpdateSource(t).URL {
			t.Fatal("credential or route leak")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/rss+xml"}}, Body: io.NopCloser(strings.NewReader(`<rss><channel><item><title>News</title></item></channel></rss>`))}, nil
	})
	if _, err := fetchCareerUpdates(context.Background(), client, careerUpdateSource(t)); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("calls=%d", calls)
	}
}

func TestCareerUsageRedirectStatusAndWindowErrorsAreSanitized(t *testing.T) {
	now := time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)
	for _, status := range []int{302, 401, 429, 500} {
		calls := 0
		client := &http.Client{Transport: careerRoundTrip(func(r *http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: status, Header: http.Header{"Location": {"https://evil.example.com/steal"}}, Body: io.NopCloser(strings.NewReader("synthetic-key provider-secret"))}, nil
		})}
		_, err := fetchCareerUsage(context.Background(), client, "synthetic-key", now)
		if err == nil || strings.Contains(err.Error(), "synthetic-key") || strings.Contains(err.Error(), "provider-secret") || calls != 1 {
			t.Fatalf("status %d: calls=%d err=%v", status, calls, err)
		}
	}
	for _, body := range []string{careerDataset("2026-09-25", "2026-10-01", []any{}), strings.Repeat("x", (2<<20)+1)} {
		client := &http.Client{Transport: careerRoundTrip(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
		})}
		if _, err := fetchCareerUsage(context.Background(), client, "synthetic-key", now); err == nil {
			t.Fatal("accepted mismatched/oversized response")
		}
	}
	client := &http.Client{Transport: careerRoundTrip(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("synthetic-key in underlying error")
	})}
	if _, err := fetchCareerUsage(context.Background(), client, "synthetic-key", now); err == nil || strings.Contains(err.Error(), "synthetic-key") {
		t.Fatalf("unsanitized transport: %v", err)
	}
	for _, key := range []string{"", "x\nInjected: yes", strings.Repeat("x", 513)} {
		if _, err := fetchCareerUsage(context.Background(), client, key, now); err == nil {
			t.Fatal("accepted invalid key")
		}
	}
}

func TestCareerUpdatesFetchRejectsRedirectsAndInvalidSources(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: careerRoundTrip(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": {"https://evil.example.com/"}}, Body: io.NopCloser(strings.NewReader("secret response"))}, nil
	})}
	_, err := fetchCareerUpdates(context.Background(), client, careerUpdateSource(t))
	if err == nil || strings.Contains(err.Error(), "secret response") || calls != 1 {
		t.Fatalf("redirect: %v %d", err, calls)
	}
	source := careerUpdateSource(t)
	source.Provider = "ashby"
	if _, err := fetchCareerUpdates(context.Background(), client, source); err == nil || calls != 1 {
		t.Fatal("forged source fetched")
	}
	source = careerUpdateSource(t)
	source.Kind = "jobs"
	if _, err := fetchCareerUpdates(context.Background(), client, source); err == nil || calls != 1 {
		t.Fatal("wrong kind fetched")
	}
}

func TestCareerUpdatesCDATAPlainText(t *testing.T) {
	body := `<rss><channel><item><title><![CDATA[Release details]]></title><description><![CDATA[Plain description without HTML]]></description></item></channel></rss>`
	got, err := parseCareerUpdates(careerUpdateSource(t), "application/rss+xml", []byte(body))
	if err != nil || len(got) != 1 || got[0].Title != "Release details" || got[0].Text != "Plain description without HTML" {
		t.Fatalf("CDATA: %+v %v", got, err)
	}
}

func TestCareerUpdatesArticlePublicationNeedsContentAndUniqueMetadata(t *testing.T) {
	valid := `<html><head><title>Company research</title><meta property="article:published_time" content="2026-10-01T09:00:00+02:00"></head><body><nav>Menu</nav><article><h1>Research</h1><p>Article findings.</p></article><aside>Unrelated sidebar</aside></body></html>`
	got, err := parseCareerUpdates(careerUpdateSource(t), "text/html", []byte(valid))
	if err != nil || len(got) != 1 || got[0].Kind != "article" || got[0].PublishedAt != "2026-10-01T07:00:00Z" || !strings.Contains(got[0].Text, "findings") || strings.Contains(got[0].Text, "sidebar") {
		t.Fatalf("article: %+v %v", got, err)
	}
	for _, raw := range []string{
		strings.Replace(valid, `<meta property="article:published_time" content="2026-10-01T09:00:00+02:00">`, "", 1),
		strings.Replace(valid, "2026-10-01T09:00:00+02:00", "2026-02-30", 1),
		strings.Replace(valid, "</head>", `<meta property="article:published_time" content="2026-10-02T00:00:00Z"></head>`, 1),
		strings.Replace(valid, "<article>", "<main>", 1),
		strings.Replace(valid, "<article><h1>Research</h1><p>Article findings.</p></article>", "<article></article>", 1),
		strings.Replace(valid, "</article>", "</article><article>Second</article>", 1),
		strings.Replace(valid, "<article>", "<article><article>", 1),
		strings.Replace(valid, "article:published_time", "article:modified_time", 1),
	} {
		got, err := parseCareerUpdates(careerUpdateSource(t), "text/html", []byte(raw))
		if err != nil || len(got) != 1 || got[0].Kind != "page-snapshot" || got[0].PublishedAt != "" {
			t.Fatalf("fallback: %+v %v", got, err)
		}
	}
}

func TestCareerUpdatesDeduplicatesGeneratedIdentity(t *testing.T) {
	got, err := parseCareerUpdates(careerUpdateSource(t), "application/rss+xml", []byte(`<rss><channel><item><title>Same</title></item><item><title>Same</title></item></channel></rss>`))
	if err != nil || len(got) != 1 || got[0].SourceID == "" {
		t.Fatalf("identity dedup: %+v %v", got, err)
	}
}
