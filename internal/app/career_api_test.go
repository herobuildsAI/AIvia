package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestCareerAPIInitialBoundaries(t *testing.T) {
	s, _ := testServer(t)
	w := apiTest(s, "GET", "/api/career", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"revision":1`) || strings.Contains(w.Body.String(), "openRouterKey") {
		t.Fatalf("view %d: %s", w.Code, w.Body.String())
	}
	if w = apiTest(s, "POST", "/api/career/usage", `{"revision":0}`); w.Code != 409 {
		t.Fatalf("stale %d: %s", w.Code, w.Body.String())
	}
	if w = apiTest(s, "POST", "/api/career/key", `{"revision":1,"key":"secret","unexpected":true}`); w.Code != 400 {
		t.Fatalf("strict JSON %d", w.Code)
	}
	w = apiTest(s, "POST", "/api/career/companies", `{"revision":1,"company":{"name":"Example","careersUrl":"https://jobs.lever.co/example"}}`)
	if w.Code != 200 {
		t.Fatalf("create %d: %s", w.Code, w.Body.String())
	}
	var c CareerState
	if err := json.Unmarshal(w.Body.Bytes(), &c); err != nil {
		t.Fatal(err)
	}
	if len(c.Companies) != 1 || c.Companies[0].ID == "" || c.Revision != 2 {
		t.Fatalf("create result %#v", c)
	}
}

// Accepting provider attribution URLs but failing freshness persistence would break refresh.
func TestCareerAPICanonicalSourcesAndOwnedFreshness(t *testing.T) {
	s, store := testServer(t)
	c := careerAPICall(t, s, "POST", "/api/career/companies", map[string]any{"revision": 1, "company": CareerCompany{Name: "Example", CareersURL: "https://jobs.lever.co/example?utm_source=site", Refreshes: []CareerRefresh{{Kind: "jobs", SourceURL: "https://jobs.lever.co/example", CheckedAt: timestamp(), LastSuccess: timestamp(), Complete: true}}}})
	if c.Companies[0].CareersURL != "https://jobs.lever.co/example" || len(c.Companies[0].Refreshes) != 0 {
		t.Fatalf("company can fabricate freshness / noncanonical URL: %#v", c.Companies[0])
	}
	s.careerTransport = careerRoundTrip(func(r *http.Request) (*http.Response, error) {
		return careerAPIResponse(200, "application/json", `[{"id":"one","text":"Software Intern","hostedUrl":"https://jobs.lever.co/example/one","descriptionPlain":"Go project","categories":{}}]`), nil
	})
	c = careerAPICall(t, s, "POST", "/api/career/refresh", map[string]any{"revision": c.Revision, "companyId": c.Companies[0].ID, "kind": "jobs"})
	if len(c.Jobs) != 1 || len(c.Companies[0].Refreshes) != 1 || c.Companies[0].Refreshes[0].LastSuccess == "" {
		t.Fatalf("refresh %#v", c)
	}
	co := c.Companies[0]
	co.Name = "New name"
	co.Refreshes = nil
	c = careerAPICall(t, s, "POST", "/api/career/companies", map[string]any{"revision": c.Revision, "company": co})
	if len(c.Companies[0].Refreshes) != 1 {
		t.Fatal("editing erased server freshness")
	}
	if store.Snapshot().Career.Revision != c.Revision {
		t.Fatal("missing save")
	}
}

func careerAPICall(t *testing.T, s *Server, method, path string, body any) CareerState {
	t.Helper()
	b, _ := json.Marshal(body)
	w := apiTest(s, method, path, string(b))
	if w.Code != 200 {
		t.Fatalf("%s %s %d: %s", method, path, w.Code, w.Body.String())
	}
	var c CareerState
	if err := json.Unmarshal(w.Body.Bytes(), &c); err != nil {
		t.Fatal(err)
	}
	return c
}
func careerAPIResponse(status int, media, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {media}}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestCareerAPIFlowPreviewModelAndExplicitEditedNote(t *testing.T) {
	s, store := testServer(t)
	modelName := strings.Repeat("m", 200) // Accepted by existing model settings; note storage must agree.
	var sent []Message
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/models" {
			writeJSON(w, 200, map[string]any{"data": []map[string]string{{"id": modelName}}})
			return
		}
		var body struct {
			Messages []Message `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		sent = body.Messages
		io.WriteString(w, `{"choices":[{"message":{"content":"Prepare a Go project [source]."},"finish_reason":"stop"}]}`)
	}))
	defer model.Close()
	settings := ModelSettings{Provider: "compatible", Endpoint: model.URL, Model: modelName, LocalConfirmed: true}
	if err := store.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	c := careerAPICall(t, s, "POST", "/api/career/companies", map[string]any{"revision": 1, "company": CareerCompany{Name: "Example"}})
	id := c.Companies[0].ID
	c = careerAPICall(t, s, "PUT", "/api/career/student", map[string]any{"revision": c.Revision, "student": StudentProfile{Skills: "Go"}})
	c = careerAPICall(t, s, "POST", "/api/career/key", map[string]any{"revision": c.Revision, "key": "dataset-private-key"})
	c = careerAPICall(t, s, "POST", "/api/career/paste", map[string]any{"revision": c.Revision, "companyId": id, "kind": "jobs", "title": "Software Intern", "url": "https://example.com/job", "text": "Build services in Go"})
	if c.Jobs[0].Provider != "paste" || c.Jobs[0].ListingState != "unknown" {
		t.Fatalf("unverified pasted listing %#v", c.Jobs[0])
	}
	selection := CareerSelection{JobIDs: []string{c.Jobs[0].ID}, IncludeStudent: true, Question: "What should I prepare?"}
	b, _ := json.Marshal(map[string]any{"revision": c.Revision, "selection": selection})
	w := apiTest(s, "POST", "/api/career/preview", string(b))
	if w.Code != 200 {
		t.Fatalf("preview %d: %s", w.Code, w.Body.String())
	}
	var preview CareerPreview
	json.Unmarshal(w.Body.Bytes(), &preview)
	if len(preview.Sources) != 1 || len(preview.Messages) != 2 || preview.Hash == "" || strings.Contains(w.Body.String(), "dataset-private-key") {
		t.Fatal("invalid preview")
	}
	b, _ = json.Marshal(map[string]any{"revision": c.Revision, "selection": selection, "settings": settings, "previewHash": "wrong"})
	w = apiTest(s, "POST", "/api/career/analyze", string(b))
	if w.Code != 409 {
		t.Fatalf("wrong hash %d", w.Code)
	}
	b, _ = json.Marshal(map[string]any{"revision": c.Revision, "selection": selection, "settings": settings, "previewHash": preview.Hash})
	w = apiTest(s, "POST", "/api/career/analyze", string(b))
	if w.Code != 200 {
		t.Fatalf("analyze %d: %s", w.Code, w.Body.String())
	}
	if !reflect.DeepEqual(sent, preview.Messages) {
		t.Fatal("sent context differs from reviewed exact messages")
	}
	if len(store.Snapshot().Career.Advice) != 0 || store.Snapshot().Career.Revision != c.Revision {
		t.Fatal("analysis persisted automatically")
	}
	c = careerAPICall(t, s, "POST", "/api/career/advice", map[string]any{"revision": c.Revision, "selection": selection, "settings": settings, "text": "My edited preparation note"})
	if len(c.Advice) != 1 || c.Advice[0].Text != "My edited preparation note" || len(c.Advice[0].Sources) != 1 {
		t.Fatalf("edited note %#v", c.Advice)
	}
	c = careerAPICall(t, s, "DELETE", "/api/career/advice/"+c.Advice[0].ID, map[string]any{"revision": c.Revision})
	c = careerAPICall(t, s, "POST", "/api/career/advice", map[string]any{"revision": c.Revision, "selection": selection, "settings": settings, "text": "Note citing company"})
	c = careerAPICall(t, s, "DELETE", "/api/career/companies/"+id, map[string]any{"revision": c.Revision})
	if len(c.Companies)+len(c.Jobs)+len(c.Updates)+len(c.Advice) != 0 {
		t.Fatal("delete did not cascade")
	}
}

func TestCareerAPIRefreshRacesCancellationAndGate(t *testing.T) {
	for _, action := range []string{"edit", "delete", "cancel"} {
		t.Run(action, func(t *testing.T) {
			s, store := testServer(t)
			c := careerAPICall(t, s, "POST", "/api/career/companies", map[string]any{"revision": 1, "company": CareerCompany{Name: "Example", CareersURL: "https://jobs.lever.co/example"}})
			started, release := make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			releaseNow := func() { releaseOnce.Do(func() { close(release) }) }
			defer releaseNow()
			s.careerTransport = careerRoundTrip(func(r *http.Request) (*http.Response, error) {
				close(started)
				<-release
				return careerAPIResponse(200, "application/json", `[]`), nil
			})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			b, _ := json.Marshal(map[string]any{"revision": c.Revision, "companyId": c.Companies[0].ID, "kind": "jobs"})
			done := make(chan *httptest.ResponseRecorder, 1)
			go func() { done <- careerAPIRequest(s, ctx, "POST", "/api/career/refresh", string(b)) }()
			<-started
			if w := apiTest(s, "POST", "/api/career/refresh", string(b)); w.Code != 400 {
				t.Fatalf("concurrent gate %d", w.Code)
			}
			switch action {
			case "edit":
				c = careerAPICall(t, s, "PUT", "/api/career/student", map[string]any{"revision": c.Revision, "student": StudentProfile{Skills: "Edited"}})
			case "delete":
				c = careerAPICall(t, s, "DELETE", "/api/career/companies/"+c.Companies[0].ID, map[string]any{"revision": c.Revision})
			case "cancel":
				cancel()
			}
			releaseNow()
			w := <-done
			want := 409
			if action == "cancel" {
				want = 400
			}
			if w.Code != want {
				t.Fatalf("%s result %d: %s", action, w.Code, w.Body.String())
			}
			got := store.Snapshot().Career
			if got.Revision != c.Revision || len(got.Jobs) != 0 || len(got.Companies) > 0 && len(got.Companies[0].Refreshes) != 0 {
				t.Fatalf("stale metadata persisted %#v", got)
			}
		})
	}
}
func careerAPIRequest(s *Server, ctx context.Context, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body)).WithContext(ctx)
	r.Host = s.opts.Host
	r.Header.Set("Origin", "http://"+s.opts.Host)
	r.Header.Set("X-Session-Token", s.token)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func TestCareerAPIPartialSourcesFailureRedactionAndRouteRefusal(t *testing.T) {
	for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"} {
		t.Setenv(key, "")
	}
	s, _ := testServer(t)
	c := careerAPICall(t, s, "POST", "/api/career/companies", map[string]any{"revision": 1, "company": CareerCompany{Name: "Example", CareersURL: "https://jobs.lever.co/example", NewsURLs: []string{"https://example.com/news", "https://example.com/failure"}}})
	c = careerAPICall(t, s, "POST", "/api/career/key", map[string]any{"revision": c.Revision, "key": "private-dataset-secret"})
	calls := 0
	s.careerTransport = careerRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Path == "/failure" {
			return nil, errors.New("private-dataset-secret")
		}
		if strings.Contains(r.URL.Path, "datasets") {
			return careerAPIResponse(401, "application/json", `private-dataset-secret`), nil
		}
		return careerAPIResponse(200, "text/plain", "Launch news"), nil
	})
	c = careerAPICall(t, s, "POST", "/api/career/refresh", map[string]any{"revision": c.Revision, "companyId": c.Companies[0].ID, "kind": "updates"})
	if len(c.Updates) != 1 || len(c.Companies[0].Refreshes) != 2 || c.Companies[0].Refreshes[0].LastSuccess == "" || c.Companies[0].Refreshes[0].Complete || c.Companies[0].Refreshes[1].Error == "" {
		t.Fatalf("per-source preservation %#v", c)
	}
	b, _ := json.Marshal(map[string]any{"revision": c.Revision})
	w := apiTest(s, "POST", "/api/career/usage", string(b))
	if w.Code != 400 || strings.Contains(w.Body.String(), "private-dataset-secret") {
		t.Fatalf("usage failure leaked %s", w.Body.String())
	}
	for _, path := range []string{"/api/career", "/api/state"} {
		w = apiTest(s, "GET", path, "")
		if strings.Contains(w.Body.String(), "private-dataset-secret") || strings.Contains(w.Body.String(), "openRouterKey") {
			t.Fatal("secret exposed")
		}
		if path == "/api/state" && strings.Contains(w.Body.String(), "Launch news") {
			t.Fatal("diagnostics received career data")
		}
	}
	previousCalls := calls
	s.opts.Proxy = "socks5://127.0.0.1:1080"
	c = careerAPICall(t, s, "POST", "/api/career/refresh", map[string]any{"revision": c.Revision, "companyId": c.Companies[0].ID, "kind": "updates"})
	if calls != previousCalls || len(c.Updates) != 1 || !strings.Contains(c.Companies[0].Refreshes[0].Error, "cannot verify destinations") {
		t.Fatal("fixture bypassed real proxy refusal / erased evidence")
	}
	c = careerAPICall(t, s, "POST", "/api/career/key", map[string]any{"revision": c.Revision, "key": ""})
	w = apiTest(s, "GET", "/api/career", "")
	if !strings.Contains(w.Body.String(), `"keyConfigured":true`) {
		t.Fatal("blank key did not preserve")
	}
	c = careerAPICall(t, s, "POST", "/api/career/key", map[string]any{"revision": c.Revision, "clearKey": true})
	w = apiTest(s, "GET", "/api/career", "")
	if !strings.Contains(w.Body.String(), `"keyConfigured":false`) {
		t.Fatal("key not cleared")
	}
}

func TestCareerAPISessionOriginStrictAndSelection(t *testing.T) {
	s, _ := testServer(t)
	for _, tc := range []struct{ method, path, origin, token string }{{"GET", "/api/career", "", ""}, {"POST", "/api/career/key", "https://evil.test", s.token}, {"POST", "/api/career/student", "", s.token}} {
		r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(`{"revision":1}`))
		r.Host = s.opts.Host
		r.Header.Set("Origin", tc.origin)
		r.Header.Set("X-Session-Token", tc.token)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatalf("boundary %d", w.Code)
		}
	}
	for _, body := range []string{`{"revision":1,"selection":{"jobIds":["missing"]}}`, `{"revision":1,"selection":{"unexpected":true}}`, `{"revision":1}{}`} {
		if w := apiTest(s, "POST", "/api/career/preview", body); w.Code != 400 {
			t.Fatalf("invalid request %d %s", w.Code, w.Body.String())
		}
	}
}

func TestCareerAPIUpdatesDeduplicateAcrossFeedsAndRetainPasted(t *testing.T) {
	s, _ := testServer(t)
	c := careerAPICall(t, s, "POST", "/api/career/companies", map[string]any{"revision": 1, "company": CareerCompany{Name: "Example"}})
	id := c.Companies[0].ID
	c = careerAPICall(t, s, "POST", "/api/career/paste", map[string]any{"revision": c.Revision, "companyId": id, "kind": "updates", "title": "My excerpt", "url": "https://example.com/article", "text": "User text"})
	c.Updates = append(c.Updates, CompanyUpdate{ID: newID(), CompanyID: id, SourceID: "first", SourceURL: "https://example.com/feed1", URL: "https://example.com/article", Title: "Article", Text: "Original", Kind: "feed-item", FetchedAt: timestamp()})
	next, err := mergeCareerUpdates(c, id, []CompanyUpdate{{SourceID: "second", SourceURL: "https://example.com/feed2", URL: "https://example.com/article", Title: "Article updated", Text: "Updated", Kind: "feed-item", FetchedAt: timestamp()}})
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Updates) != 2 || next.Updates[0].Text != "User text" || next.Updates[1].ID != c.Updates[1].ID || next.Updates[1].Text != "Updated" {
		t.Fatalf("article duplicated / pasted data overwritten: %#v", next.Updates)
	}
}

func TestCareerAPIAnalyzeDiscardsBlockedStaleAndCanceledAnswers(t *testing.T) {
	for _, action := range []string{"settings", "edit", "cancel"} {
		t.Run(action, func(t *testing.T) {
			s, store := testServer(t)
			started, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			releaseNow := func() { once.Do(func() { close(release) }) }
			defer releaseNow()
			model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/v1/models" {
					io.WriteString(w, `{"data":[{"id":"local"}]}`)
					return
				}
				close(started)
				<-release
				io.WriteString(w, `{"choices":[{"message":{"content":"Stale answer"},"finish_reason":"stop"}]}`)
			}))
			defer func() { releaseNow(); model.Close() }()
			settings := ModelSettings{Provider: "compatible", Endpoint: model.URL, Model: "local", LocalConfirmed: true}
			store.SaveSettings(settings)
			preview, err := careerMessages(*store.Snapshot().Career, CareerSelection{Question: "Prepare?"})
			if err != nil {
				t.Fatal(err)
			}
			b, _ := json.Marshal(map[string]any{"revision": 1, "selection": CareerSelection{Question: "Prepare?"}, "previewHash": preview.Hash, "settings": settings})
			// The same slot excludes diagnostic/research model work and Career analysis.
			s.chatGate <- struct{}{}
			w := apiTest(s, "POST", "/api/career/analyze", string(b))
			<-s.chatGate
			if w.Code != 400 {
				t.Fatalf("shared chat gate %d", w.Code)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan *httptest.ResponseRecorder, 1)
			go func() { done <- careerAPIRequest(s, ctx, "POST", "/api/career/analyze", string(b)) }()
			<-started
			switch action {
			case "settings":
				settings.LocalConfirmed = false
				store.SaveSettings(settings)
			case "edit":
				careerAPICall(t, s, "PUT", "/api/career/student", map[string]any{"revision": 1, "student": StudentProfile{Skills: "Edited"}})
			case "cancel":
				cancel()
			}
			releaseNow()
			w = <-done
			want := 409
			if action == "cancel" {
				want = 400
			}
			if w.Code != want || strings.Contains(w.Body.String(), "Stale answer") {
				t.Fatalf("%s %d %s", action, w.Code, w.Body.String())
			}
			if len(store.Snapshot().Career.Advice) != 0 {
				t.Fatal("stale analysis auto-saved")
			}
		})
	}
}

func TestCareerAPIUsageFixedWindowAndStaleBeforeNetwork(t *testing.T) {
	s, store := testServer(t)
	c := careerAPICall(t, s, "POST", "/api/career/key", map[string]any{"revision": 1, "key": "fixture-dataset-key"})
	calls := 0
	s.careerTransport = careerRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Host != "openrouter.ai" || r.URL.Path != "/api/v1/datasets/rankings-daily" || r.Header.Get("Authorization") != "Bearer fixture-dataset-key" {
			t.Fatal("dataset escaped fixed boundary")
		}
		start, end := r.URL.Query().Get("start_date"), r.URL.Query().Get("end_date")
		body, _ := json.Marshal(map[string]any{"data": []map[string]string{{"date": end, "model_permaslug": "example/model", "total_tokens": "90071992547409930000"}}, "meta": map[string]string{"start_date": start, "end_date": end, "version": "1", "as_of": timestamp()}})
		return careerAPIResponse(200, "application/json", string(body)), nil
	})
	w := apiTest(s, "POST", "/api/career/usage", `{"revision":1}`)
	if w.Code != 409 || calls != 0 {
		t.Fatal("stale request reached dataset")
	}
	c = careerAPICall(t, s, "POST", "/api/career/usage", map[string]any{"revision": c.Revision})
	if calls != 1 || c.Usage.Rows[0].TotalTokens != "90071992547409930000" || c.Usage.SourceURL != "https://openrouter.ai/rankings" {
		t.Fatalf("usage %#v", c.Usage)
	}
	s.careerTransport = careerRoundTrip(func(r *http.Request) (*http.Response, error) {
		return careerAPIResponse(429, "text/plain", "fixture-dataset-key"), nil
	})
	b, _ := json.Marshal(map[string]any{"revision": c.Revision})
	w = apiTest(s, "POST", "/api/career/usage", string(b))
	if w.Code != 400 || !strings.Contains(w.Body.String(), "429") || strings.Contains(w.Body.String(), "fixture-dataset-key") || store.Snapshot().Career.Revision != c.Revision || len(store.Snapshot().Career.Usage.Rows) != 1 {
		t.Fatalf("failure erased/leaked snapshot %d %s", w.Code, w.Body.String())
	}
}

func TestCareerAPIManualCodexAndChangedPreviewSettings(t *testing.T) {
	s, store := testServer(t)
	settings := ModelSettings{Provider: "codex-cli", Model: "default", CloudConfirmed: true}
	store.SaveSettings(settings)
	w := apiTest(s, "POST", "/api/career/preview", `{"revision":1,"selection":{}}`)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"delivery":"manual"`) {
		t.Fatalf("manual preview %d %s", w.Code, w.Body.String())
	}
	var preview CareerPreview
	json.Unmarshal(w.Body.Bytes(), &preview)
	b, _ := json.Marshal(map[string]any{"revision": 1, "selection": CareerSelection{}, "previewHash": preview.Hash, "settings": settings})
	w = apiTest(s, "POST", "/api/career/analyze", string(b))
	if w.Code != 400 || !strings.Contains(w.Body.String(), "manual") {
		t.Fatalf("Codex invoked %d %s", w.Code, w.Body.String())
	}
	c := careerAPICall(t, s, "POST", "/api/career/advice", map[string]any{"revision": 1, "selection": CareerSelection{}, "settings": settings, "text": "My manually edited note"})
	settings.CloudConfirmed = false
	store.SaveSettings(settings)
	b, _ = json.Marshal(map[string]any{"revision": c.Revision, "selection": CareerSelection{}, "text": "Should not save", "settings": ModelSettings{Provider: "codex-cli", Model: "default", CloudConfirmed: true}})
	w = apiTest(s, "POST", "/api/career/advice", string(b))
	if w.Code != 409 || len(store.Snapshot().Career.Advice) != 1 {
		t.Fatal("saved stale settings metadata")
	}
}

func TestCareerAPICapacityFailureDoesNotPersistPartialRefresh(t *testing.T) {
	for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"} {
		t.Setenv(key, "")
	}
	s, store := testServer(t)
	c := careerAPICall(t, s, "POST", "/api/career/companies", map[string]any{"revision": 1, "company": CareerCompany{Name: "Example", NewsURLs: []string{"https://example.com/new"}}})
	for i := 0; i < 300; i++ {
		c.Updates = append(c.Updates, CompanyUpdate{ID: newID(), CompanyID: c.Companies[0].ID, Title: "Pasted update", Text: "Preserved", Kind: "pasted", FetchedAt: timestamp()})
	}
	var err error
	c, err = store.SaveCareer(c, c.Revision)
	if err != nil {
		t.Fatal(err)
	}
	s.careerTransport = careerRoundTrip(func(r *http.Request) (*http.Response, error) {
		return careerAPIResponse(200, "text/plain", "New article"), nil
	})
	b, _ := json.Marshal(map[string]any{"revision": c.Revision, "companyId": c.Companies[0].ID, "kind": "updates"})
	w := apiTest(s, "POST", "/api/career/refresh", string(b))
	if w.Code != 400 || store.Snapshot().Career.Revision != c.Revision || len(store.Snapshot().Career.Companies[0].Refreshes) != 0 {
		t.Fatalf("capacity produced partial state %d %s", w.Code, w.Body.String())
	}
}

// Removing the under-lock context check would save a request canceled while waiting for Store.
func TestCareerAPIStoreCanceledWhileWaitingForLock(t *testing.T) {
	s, store := testServer(t)
	c := *store.Snapshot().Career
	c.Student.Skills = "Must not save"
	base, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx := &careerAPIObservedContext{Context: base, checked: make(chan struct{})}
	r := httptest.NewRequest("POST", "/api/career/student", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	store.mu.Lock()
	done := make(chan struct{})
	go func() { s.saveCareer(w, r, c, c.Revision); close(done) }()
	<-ctx.checked
	cancel()
	store.mu.Unlock()
	<-done
	if w.Code != 400 || store.Snapshot().Career.Revision != 1 || store.Snapshot().Career.Student.Skills != "" {
		t.Fatalf("canceled lock wait saved: %d %s", w.Code, w.Body.String())
	}
}

type careerAPIObservedContext struct {
	context.Context
	checked chan struct{}
	once    sync.Once
}

func (c *careerAPIObservedContext) Err() error {
	err := c.Context.Err()
	c.once.Do(func() { close(c.checked) })
	return err
}

func TestCareerAPIStoreRejectsSettingsChangedWhileWaitingForLock(t *testing.T) {
	_, store := testServer(t)
	c := *store.Snapshot().Career
	settings := store.Snapshot().Settings
	c.Student.Skills = "Must not save"
	store.mu.Lock()
	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		close(started)
		_, err := store.saveCareer(context.Background(), c, c.Revision, &settings)
		done <- err
	}()
	<-started
	next := cloneState(store.state)
	next.Settings.LocalConfirmed = !settings.LocalConfirmed
	if err := store.persist(next); err != nil {
		store.mu.Unlock()
		t.Fatal(err)
	}
	store.mu.Unlock()
	if err := <-done; !errors.Is(err, ErrConflict) {
		t.Fatalf("settings guard returned %v", err)
	}
	if store.Snapshot().Career.Revision != 1 || store.Snapshot().Career.Student.Skills != "" {
		t.Fatal("settings-changed note was persisted")
	}
}

func TestCareerAPIClaudePreviewAnalyzeExactCLIContext(t *testing.T) {
	s, store := testServer(t)
	cliPath := cliFixture(t, "career-api-success")
	settings := ModelSettings{Provider: "claude-cli", Model: "default", CLIPath: cliPath, CloudConfirmed: true}
	if err := store.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	c := careerAPICall(t, s, "POST", "/api/career/companies", map[string]any{"revision": 1, "company": CareerCompany{Name: "Selected company"}})
	c = careerAPICall(t, s, "PUT", "/api/career/student", map[string]any{"revision": c.Revision, "student": StudentProfile{Projects: "SYNTHETIC_PRIVATE_PROJECT"}})
	c = careerAPICall(t, s, "POST", "/api/career/key", map[string]any{"revision": c.Revision, "key": "SYNTHETIC_PRIVATE_KEY"})
	c = careerAPICall(t, s, "POST", "/api/career/paste", map[string]any{"revision": c.Revision, "companyId": c.Companies[0].ID, "kind": "jobs", "title": "Selected internship", "text": "Selected Go requirements", "url": "https://example.com/job"})
	selection := CareerSelection{JobIDs: []string{c.Jobs[0].ID}, Question: "Which project should I prepare?"}
	body, _ := json.Marshal(map[string]any{"revision": c.Revision, "selection": selection})
	w := apiTest(s, "POST", "/api/career/preview", string(body))
	if w.Code != 200 {
		t.Fatalf("Claude preview %d: %s", w.Code, w.Body.String())
	}
	var preview struct {
		CareerPreview
		Settings ModelSettings `json:"settings"`
		Delivery string        `json:"delivery"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}
	if preview.Delivery != "direct" || preview.Settings != settings || len(preview.Messages) != 2 || preview.Messages[0].Content != careerInstructions {
		t.Fatal("Claude preview did not retain its trusted Career instructions and destination")
	}
	body, _ = json.Marshal(map[string]any{"revision": preview.Revision, "selection": selection, "settings": preview.Settings, "previewHash": preview.Hash})
	w = apiTest(s, "POST", "/api/career/analyze", string(body))
	if w.Code != 200 {
		t.Fatalf("Claude analyze %d: %s", w.Code, w.Body.String())
	}
	var answer struct {
		Reply string `json:"reply"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &answer); err != nil || answer.Reply != "Check finding F1." {
		t.Fatal("Claude fixture response was not returned")
	}
	traceBytes, err := os.ReadFile(filepath.Join(filepath.Dir(cliPath), "trace.json"))
	if err != nil {
		t.Fatal(err)
	}
	var trace struct {
		Args  []string
		Stdin string
	}
	if err := json.Unmarshal(traceBytes, &trace); err != nil {
		t.Fatal(err)
	}
	wantArgs := []string{"-p", "--output-format", "json", "--safe-mode", "--restricted", "--tools", "", "--disallowedTools", "*", "--strict-mcp-config", "--mcp-config", `{"mcpServers":{}}`, "--disable-slash-commands", "--settings", `{"disableAllHooks":true,"disableClaudeAiConnectors":true}`, "--setting-sources", "", "--no-session-persistence", "--no-chrome", "--permission-mode", "dontAsk", "--system-prompt", careerInstructions}
	if !reflect.DeepEqual(trace.Args, wantArgs) {
		t.Fatal("Claude API arguments changed trusted instructions or isolation")
	}
	wantStdin, _ := json.Marshal(struct {
		Messages []Message `json:"messages"`
	}{preview.Messages[1:]})
	if trace.Stdin != string(wantStdin) || strings.Contains(string(traceBytes), "SYNTHETIC_PRIVATE") {
		t.Fatal("Claude API sent unreviewed private context or changed the exact preview")
	}
	if store.Snapshot().Career.Revision != c.Revision || len(store.Snapshot().Career.Advice) != 0 {
		t.Fatal("Claude analysis persisted an unrequested note")
	}
}
