package app

import (
	"encoding/json"
	"mime"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

func testServer(t *testing.T) (*Server, *Store) {
	t.Helper()
	s, e := OpenStore(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	srv, e := NewServer(s, Options{Host: "127.0.0.1:9000", STUN: "stun:stun.l.google.com:19302"})
	if e != nil {
		t.Fatal(e)
	}
	return srv, s
}
func apiTest(s *Server, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Host = s.opts.Host
	r.Header.Set("Origin", "http://"+s.opts.Host)
	r.Header.Set("X-Session-Token", s.token)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}
func TestServerBoundaries(t *testing.T) {
	s, _ := testServer(t)
	for _, test := range []struct {
		host, origin, token string
		method, path        string
	}{{"evil.test", "", s.token, "GET", "/api/session"}, {s.opts.Host, "https://evil.test", s.token, "POST", "/api/profiles"}, {s.opts.Host, "", "", "GET", "/api/state"}, {s.opts.Host, "", s.token, "POST", "/api/profiles"}} {
		r := httptest.NewRequest(test.method, test.path, strings.NewReader(`{}`))
		r.Host = test.host
		r.Header.Set("Origin", test.origin)
		r.Header.Set("X-Session-Token", test.token)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatalf("boundary accepted: %#v status %d", test, w.Code)
		}
	}
	if w := apiTest(s, "POST", "/api/profiles", `{"name":"Muse","kind":"app","unexpected":"secret"}`); w.Code != 400 {
		t.Fatalf("unknown JSON field %d", w.Code)
	}
	if w := apiTest(s, "POST", "/api/profiles", strings.Repeat("x", 300<<10)); w.Code != 400 {
		t.Fatalf("oversized input %d", w.Code)
	}
}
func TestServerOfflineReportAndSave(t *testing.T) {
	s, store := testServer(t)
	p, e := store.SaveProfile(Profile{Name: "Muse", Kind: "app"})
	if e != nil {
		t.Fatal(e)
	}
	b, _ := json.Marshal(RunInput{ProfileID: p.ID, Revision: p.Revision, Browser: BrowserEvidence{Timezone: "UTC", OffsetMinutes: ptr(0)}})
	w := apiTest(s, "POST", "/api/run", string(b))
	if w.Code != 200 {
		t.Fatalf("run: %d %s", w.Code, w.Body.String())
	}
	var r Report
	if e = json.Unmarshal(w.Body.Bytes(), &r); e != nil {
		t.Fatal(e)
	}
	if len(r.Evidence.Exits) != 0 || r.Upper == r.Lower {
		t.Fatal("offline evidence fabricated")
	}
	b, _ = json.Marshal(map[string]string{"profileId": p.ID, "reportId": r.ID})
	w = apiTest(s, "POST", "/api/reports", string(b))
	if w.Code != 200 || len(store.Snapshot().Reports) != 1 {
		t.Fatalf("save: %s", w.Body.String())
	}
	w = apiTest(s, "GET", "/api/reports/"+r.ID, "")
	if w.Code != 200 || strings.Contains(w.Body.String(), `"evidence"`) {
		t.Fatal("raw evidence exported")
	}
}
func TestChatIsolation(t *testing.T) {
	var sent string
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			w.Write([]byte(`{"data":[{"id":"local"}]}`))
			return
		}
		var payload struct {
			Messages []Message `json:"messages"`
		}
		json.NewDecoder(r.Body).Decode(&payload)
		for _, m := range payload.Messages {
			sent += m.Content
		}
		w.Write([]byte(`{"choices":[{"message":{"content":"Inspect the unknown findings."}}]}`))
	}))
	defer model.Close()
	s, store := testServer(t)
	p, _ := store.SaveProfile(Profile{Name: "Muse", Kind: "app", Notes: "FIRST PRIVATE"})
	other, _ := store.SaveProfile(Profile{Name: "Other", Kind: "app", Notes: "OTHER SECRET"})
	_ = other
	store.SaveSettings(ModelSettings{Provider: "compatible", Endpoint: model.URL + "/v1", Model: "local", LocalConfirmed: true})
	report := Score(Evidence{Exits: []Exit{{Path: "browser", IP: "8.8.8.8"}}}, Policy{Mode: "unknown"}, scoreTime)
	report.ProfileID = p.ID
	report.ProfileRevision = p.Revision
	s.current[p.ID] = report
	b, _ := json.Marshal(map[string]string{"profileId": p.ID, "reportId": report.ID})
	w := apiTest(s, "POST", "/api/context", string(b))
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var v struct {
		ID      string `json:"id"`
		Context string `json:"context"`
	}
	json.Unmarshal(w.Body.Bytes(), &v)
	b, _ = json.Marshal(map[string]string{"contextId": v.ID, "message": "What should I check?"})
	w = apiTest(s, "POST", "/api/chat", string(b))
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	for _, secret := range []string{"OTHER SECRET", "FIRST PRIVATE", "8.8.8.8"} {
		if strings.Contains(sent, secret) {
			t.Fatalf("leaked %s", secret)
		}
	}
	if !strings.Contains(sent, "unknown") {
		t.Fatal("unknown evidence removed")
	}
	b, _ = json.Marshal(map[string]string{"profileId": other.ID, "reportId": report.ID})
	if w = apiTest(s, "POST", "/api/context", string(b)); w.Code != 400 {
		t.Fatal("cross-profile report accepted")
	}
}

func TestRunCanonicalAddresses(t *testing.T) {
	in := RunInput{Network: true, WebRTC: true, Exits: []Exit{{Path: "webrtc", Family: "ipv6", IP: "2606:4700:4700:0:0:0:0:1111"}}}
	if err := validateRun(&in); err != nil {
		t.Fatal(err)
	}
	if in.Exits[0].IP != "2606:4700:4700::1111" {
		t.Fatal("address was not canonicalized before collection")
	}
	in.Exits = append(in.Exits, Exit{Path: "webrtc", Family: "ipv6", IP: "2606:4700:4700:0:0:0:0:1111"})
	if err := validateRun(&in); err == nil {
		t.Fatal("duplicate address spelling accepted")
	}
}

func TestIPSettingsAPISecretsAndIdentity(t *testing.T) {
	s, _ := testServer(t)
	secret := "private-test-key"
	w := apiTest(s, "POST", "/api/ip-settings", `{"provider":"ipapi","endpoint":"https://api.ipapi.is/","apiKey":"`+secret+`","revision":0}`)
	if w.Code != 200 {
		t.Fatalf("IP settings unavailable: %d %s", w.Code, w.Body.String())
	}
	for _, path := range []string{"/api/ip-settings", "/api/state", "/api/session"} {
		w = apiTest(s, "GET", path, "")
		if w.Code != 200 || strings.Contains(w.Body.String(), secret) {
			t.Fatalf("unsafe response at %s", path)
		}
	}
	w = apiTest(s, "POST", "/api/ip-settings", `{"provider":"ipapi","endpoint":"https://api.ipapi.is/","apiKey":"","revision":1}`)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"keyConfigured":true`) {
		t.Fatal("blank key did not retain the saved credential")
	}
	w = apiTest(s, "POST", "/api/ip-settings", `{"provider":"custom","endpoint":"http://127.0.0.1:9191/lookup","apiKey":"","revision":2}`)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"keyConfigured":false`) {
		t.Fatal("previous provider key carried to a new destination")
	}
	w = apiTest(s, "POST", "/api/ip-settings", `{"provider":"ipapi","endpoint":"https://api.ipapi.is/","apiKey":"","revision":1}`)
	if w.Code != 409 {
		t.Fatal("stale IP settings accepted")
	}
}

func TestIPSettingsAPIEnvironmentKeyNeverMoves(t *testing.T) {
	s, _ := testServer(t)
	s.opts.IPKey = "environment-secret"
	w := apiTest(s, "GET", "/api/ip-settings", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"keyConfigured":true`) {
		t.Fatal("environment compatibility missing")
	}
	w = apiTest(s, "POST", "/api/ip-settings", `{"provider":"custom","endpoint":"http://127.0.0.1:9191/lookup","revision":0}`)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"keyConfigured":false`) || strings.Contains(w.Body.String(), "environment-secret") {
		t.Fatal("environment credential leaked into custom provider")
	}
}

func TestCLISettingsRequireCloudConsent(t *testing.T) {
	s, store := testServer(t)
	w := apiTest(s, "POST", "/api/settings", `{"provider":"claude-cli","model":"default","localConfirmed":true,"cloudConfirmed":false}`)
	if w.Code != 200 {
		t.Fatalf("CLI settings not supported: %s", w.Body.String())
	}
	b, _ := json.Marshal(store.Snapshot().Settings)
	if strings.Contains(string(b), `"localConfirmed":true`) {
		t.Fatal("CLI was mislabeled as local inference")
	}
}

func TestIPProviderRevisionRequiredBeforeNetwork(t *testing.T) {
	s, store := testServer(t)
	p, _ := store.SaveProfile(Profile{Name: "Muse", Kind: "app"})
	if err := store.SaveIPSettings(IPSettings{Provider: "custom", Endpoint: "http://127.0.0.1:9191/"}); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(RunInput{ProfileID: p.ID, Revision: p.Revision, Network: true, Intelligence: true})
	w := apiTest(s, "POST", "/api/run", string(b))
	if w.Code != 400 || !strings.Contains(w.Body.String(), "IP provider settings changed") {
		t.Fatalf("stale provider consent accepted: %s", w.Body.String())
	}
}

func TestIPKeyRemovalAndExportIsolation(t *testing.T) {
	s, store := testServer(t)
	p, _ := store.SaveProfile(Profile{Name: "Muse", Kind: "app"})
	secret := "saved-ip-key-private"
	w := apiTest(s, "POST", "/api/ip-settings", `{"provider":"ipapi","apiKey":"`+secret+`","revision":0}`)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	report := Score(Evidence{}, Policy{Mode: "unknown"}, scoreTime)
	report.ProfileID = p.ID
	report.ProfileRevision = p.Revision
	s.current[p.ID] = report
	b, _ := json.Marshal(map[string]string{"profileId": p.ID, "reportId": report.ID})
	for _, w := range []*httptest.ResponseRecorder{apiTest(s, "GET", "/api/profiles/"+p.ID+"/export", ""), apiTest(s, "POST", "/api/context", string(b))} {
		if w.Code != 200 || strings.Contains(w.Body.String(), secret) {
			t.Fatal("IP key leaked outside settings storage")
		}
	}
	w = apiTest(s, "POST", "/api/ip-settings", `{"provider":"ipapi","clearKey":true,"revision":1}`)
	if w.Code != 200 || store.Snapshot().IPSettings.APIKey != "" {
		t.Fatal("saved key not removed")
	}
}

func TestIPSettingsRejectDefaultSelfPort(t *testing.T) {
	s, _ := testServer(t)
	s.opts.Host = "127.0.0.1:80"
	w := apiTest(s, "POST", "/api/ip-settings", `{"provider":"custom","endpoint":"http://127.0.0.1/","revision":0}`)
	if w.Code != 400 {
		t.Fatal("default-port self endpoint accepted")
	}
}

func TestDirectChatWithoutReportKeepsPrivateDataOut(t *testing.T) {
	sent := make(chan []Message, 2)
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			w.Write([]byte(`{"data":[{"id":"local"}]}`))
			return
		}
		var payload struct {
			Messages []Message `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		sent <- payload.Messages
		w.Write([]byte(`{"choices":[{"message":{"content":"A 403 alone does not establish a region restriction."}}]}`))
	}))
	defer model.Close()
	s, store := testServer(t)
	if err := store.SaveSettings(ModelSettings{Provider: "compatible", Endpoint: model.URL, Model: "local", LocalConfirmed: true}); err != nil {
		t.Fatal(err)
	}
	w := apiTest(s, "POST", "/api/context", `{"mode":"direct","stage":"login","errorText":"HTTP 403"}`)
	if w.Code != 200 {
		t.Fatalf("direct conversation requires no profile or report: %s", w.Body.String())
	}
	var v struct{ ID, Context, ReportID string }
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	if v.ReportID != "" || !strings.Contains(v.Context, "HTTP 403") {
		t.Fatal("direct issue context missing or report attached")
	}
	p, err := store.SaveProfile(Profile{Name: "PRIVATE SERVICE", Kind: "app", Notes: "PRIVATE NOTES"})
	if err != nil {
		t.Fatal(err)
	}
	r := Score(Evidence{Exits: []Exit{{Path: "browser", IP: "8.8.8.8"}}}, Policy{Mode: "unknown"}, scoreTime)
	r.ProfileID, r.ProfileRevision = p.ID, p.Revision
	s.current[p.ID] = r
	for _, question := range []string{"What should I check?", "Which check first?"} {
		b, _ := json.Marshal(map[string]string{"contextId": v.ID, "message": question})
		w = apiTest(s, "POST", "/api/chat", string(b))
		if w.Code != 200 {
			t.Fatal(w.Body.String())
		}
		messages := <-sent
		encoded, _ := json.Marshal(messages)
		for _, private := range []string{"PRIVATE SERVICE", "PRIVATE NOTES", "8.8.8.8"} {
			if strings.Contains(string(encoded), private) {
				t.Fatal("direct chat included unselected data")
			}
		}
		if !strings.Contains(string(encoded), "HTTP 403") {
			t.Fatal("issue not sent")
		}
		if question == "Which check first?" && (len(messages) != 5 || messages[3].Role != "assistant") {
			t.Fatalf("conversation history lost: %#v", messages)
		}
	}
	if w = apiTest(s, "POST", "/api/settings", `{"provider":"compatible","endpoint":"`+model.URL+`","model":"local","localConfirmed":false}`); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	b, _ := json.Marshal(map[string]string{"contextId": v.ID, "message": "Stale context"})
	if w = apiTest(s, "POST", "/api/chat", string(b)); w.Code != 400 {
		t.Fatal("old context survived settings change")
	}
	if len(sent) != 0 {
		t.Fatal("stale context sent to model")
	}
}

func TestConversationContextValidation(t *testing.T) {
	s, _ := testServer(t)
	for _, body := range []string{
		`{"mode":"invalid"}`, `{"mode":"direct","reportId":"hidden-report"}`,
		`{"mode":"direct","profileId":"hidden-profile"}`, `{"mode":"direct","notes":"hidden-notes"}`,
		`{"mode":"direct","stage":"invented"}`, `{"mode":"report"}`,
		`{"mode":"direct","errorText":"` + strings.Repeat("x", 8193) + `"}`,
	} {
		if w := apiTest(s, "POST", "/api/context", body); w.Code != 400 {
			t.Fatalf("invalid context accepted: %s", body[:min(len(body), 80)])
		}
	}
}

func TestServerEntryScriptsAreServed(t *testing.T) {
	s, _ := testServer(t)
	page := apiTest(s, "GET", "/", "")
	if page.Code != http.StatusOK {
		t.Fatalf("entry page: status %d", page.Code)
	}
	scripts := regexp.MustCompile(`<script[^>]+src="([^"]+)"`).FindAllStringSubmatch(page.Body.String(), -1)
	if len(scripts) == 0 {
		t.Fatal("entry page has no scripts to bootstrap the workspace")
	}
	for _, script := range scripts {
		asset := apiTest(s, "GET", script[1], "")
		mediaType, _, err := mime.ParseMediaType(asset.Header().Get("Content-Type"))
		if asset.Code != http.StatusOK || err != nil || (mediaType != "text/javascript" && mediaType != "application/javascript") || asset.Body.Len() == 0 {
			t.Fatalf("entry script %s is not served as JavaScript: status %d, content type %q", script[1], asset.Code, asset.Header().Get("Content-Type"))
		}
	}
	for _, name := range []string{"/missing.js", "/web/career.js", "/server.go"} {
		if asset := apiTest(s, "GET", name, ""); asset.Code != http.StatusNotFound {
			t.Fatalf("unexpected static file %s: status %d", name, asset.Code)
		}
	}
}
