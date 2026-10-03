package app

import (
	"context"
	"crypto/subtle"
	"embed"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed web/*
var webFiles embed.FS

type conversation struct {
	ID, ProfileID, ReportID, Context string
	Settings                         ModelSettings
	Messages                         []Message
	Created                          time.Time
}
type Server struct {
	store                                       *Store
	opts                                        Options
	token                                       string
	mu                                          sync.Mutex
	current                                     map[string]Report
	conversations                               map[string]*conversation
	runGate, chatGate, researchGate, careerGate chan struct{}
	// Optional per-server fixture transport, applied after the real route factory.
	careerTransport http.RoundTripper
}

func NewServer(store *Store, opts Options) (*Server, error) {
	host, _, err := net.SplitHostPort(opts.Host)
	if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
		return nil, errors.New("The application must listen on a loopback address.")
	}
	if opts.STUN == "" {
		opts.STUN = "stun:stun.l.google.com:19302"
	}
	if !strings.HasPrefix(opts.STUN, "stun:") || strings.ContainsAny(opts.STUN, "@\r\n\t ") || len(opts.STUN) > 200 {
		return nil, errors.New("Invalid STUN address.")
	}
	if _, err := networkClient(opts); err != nil {
		return nil, err
	}
	return &Server{store: store, opts: opts, token: newID() + newID(), current: map[string]Report{}, conversations: map[string]*conversation{}, runGate: make(chan struct{}, 1), chatGate: make(chan struct{}, 1), researchGate: make(chan struct{}, 1), careerGate: make(chan struct{}, 1)}, nil
}
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	if errors.Is(err, ErrConflict) {
		status = http.StatusConflict
	}
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		fail(w, errors.New("Use application/json."))
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 256<<10)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		fail(w, errors.New("Request is invalid, contains unknown fields, or exceeds 256 KiB."))
		return false
	}
	if dec.Decode(new(any)) != io.EOF {
		fail(w, errors.New("Only one JSON value is allowed."))
		return false
	}
	return true
}
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	s.registerCareerRoutes(mux)
	mux.HandleFunc("GET /api/session", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"token": s.token, "ipKeyConfigured": s.ipSettingsView(s.store.Snapshot().IPSettings)["keyConfigured"], "stun": s.opts.STUN, "transport": "Agent requests use an explicit proxy or HTTP proxy environment settings; otherwise the default route. OS/PAC settings are not automatically applied."})
	})
	mux.HandleFunc("GET /api/state", func(w http.ResponseWriter, r *http.Request) {
		st := s.store.Snapshot()
		st.Career = nil
		st.IPSettings.APIKey = ""
		writeJSON(w, 200, st)
	})
	mux.HandleFunc("POST /api/profiles", func(w http.ResponseWriter, r *http.Request) {
		var p Profile
		if !readJSON(w, r, &p) {
			return
		}
		v, err := s.store.SaveProfile(p)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, v)
	})
	mux.HandleFunc("DELETE /api/profiles/{id}", func(w http.ResponseWriter, r *http.Request) {
		revision, err := strconv.Atoi(r.URL.Query().Get("revision"))
		if err != nil {
			fail(w, ErrConflict)
			return
		}
		id := r.PathValue("id")
		if err = s.store.DeleteProfile(id, revision); err != nil {
			fail(w, err)
			return
		}
		s.mu.Lock()
		delete(s.current, id)
		for key, c := range s.conversations {
			if c.ProfileID == id {
				delete(s.conversations, key)
			}
		}
		s.mu.Unlock()
		writeJSON(w, 200, map[string]bool{"deleted": true})
	})
	mux.HandleFunc("GET /api/profiles/{id}/export", s.exportProfile)
	mux.HandleFunc("GET /api/profiles/{id}/template", s.exportTemplate)
	mux.HandleFunc("POST /api/templates/preview", s.previewTemplate)
	mux.HandleFunc("GET /api/profiles/{id}/research-sources", s.researchSources)
	mux.HandleFunc("POST /api/research", s.research)
	mux.HandleFunc("POST /api/run", s.run)
	mux.HandleFunc("POST /api/reports", func(w http.ResponseWriter, r *http.Request) {
		var v struct {
			ProfileID string `json:"profileId"`
			ReportID  string `json:"reportId"`
		}
		if !readJSON(w, r, &v) {
			return
		}
		report, err := s.findReport(v.ProfileID, v.ReportID)
		if err != nil {
			fail(w, err)
			return
		}
		if err = s.store.SaveReport(report); err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, report)
	})
	mux.HandleFunc("GET /api/reports/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		s.mu.Lock()
		var found *RedactedReport
		for _, v := range s.current {
			if v.ID == id {
				x := Redact(v)
				found = &x
				break
			}
		}
		s.mu.Unlock()
		if found == nil {
			for _, v := range s.store.Snapshot().Reports {
				if v.ID == id {
					x := v
					found = &x
					break
				}
			}
		}
		if found == nil {
			fail(w, errors.New("Report not found."))
			return
		}
		w.Header().Set("Content-Disposition", `attachment; filename="diagnostic-report.json"`)
		writeJSON(w, 200, found)
	})
	mux.HandleFunc("DELETE /api/reports/{id}", func(w http.ResponseWriter, r *http.Request) {
		if err := s.store.DeleteReport(r.PathValue("id")); err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, map[string]bool{"deleted": true})
	})
	mux.HandleFunc("GET /api/ip-settings", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, s.ipSettingsView(s.store.Snapshot().IPSettings))
	})
	mux.HandleFunc("POST /api/ip-settings", s.saveIPSettings)
	mux.HandleFunc("POST /api/cli/discover", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, DiscoverCLIs(r.Context())) })
	mux.HandleFunc("POST /api/settings", s.saveSettings)
	mux.HandleFunc("GET /api/models", func(w http.ResponseWriter, r *http.Request) {
		settings := s.store.Snapshot().Settings
		c, err := s.modelClient(settings)
		if err != nil {
			fail(w, err)
			return
		}
		defer c.CloseIdleConnections()
		models, err := c.Models(r.Context())
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, models)
	})
	mux.HandleFunc("POST /api/context", s.previewContext)
	mux.HandleFunc("POST /api/chat", s.chat)
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Path
		if name == "/" {
			name = "/index.html"
		}
		if name != "/index.html" && name != "/app.js" && name != "/career.js" && name != "/style.css" {
			http.NotFound(w, r)
			return
		}
		b, err := webFiles.ReadFile("web" + name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", mime.TypeByExtension(path.Ext(name)))
		w.Write(b)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self' https://api.ipify.org https://api6.ipify.org; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		origin := r.Header.Get("Origin")
		if r.Host != s.opts.Host || (origin != "" && origin != "http://"+s.opts.Host) || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			writeJSON(w, 403, map[string]string{"error": "Untrusted host or origin."})
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") && r.URL.Path != "/api/session" {
			if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Session-Token")), []byte(s.token)) != 1 {
				writeJSON(w, 403, map[string]string{"error": "Session expired. Reload this local page."})
				return
			}
			if r.Method != "GET" && origin != "http://"+s.opts.Host {
				writeJSON(w, 403, map[string]string{"error": "A same-origin request is required."})
				return
			}
		}
		mux.ServeHTTP(w, r)
	})
}
func (s *Server) profile(id string) (Profile, error) {
	for _, p := range s.store.Snapshot().Profiles {
		if p.ID == id {
			return p, nil
		}
	}
	return Profile{}, errors.New("Service not found.")
}
func validateRun(in *RunInput) error {
	if len(in.Browser.Timezone) > 160 || len(in.Browser.Languages) > 20 || in.Browser.OffsetMinutes != nil && (*in.Browser.OffsetMinutes < -840 || *in.Browser.OffsetMinutes > 840) {
		return errors.New("Invalid browser environment.")
	}
	for _, language := range in.Browser.Languages {
		if len(language) > 80 {
			return errors.New("Invalid browser language.")
		}
	}
	if !in.Network && (in.Intelligence || in.WebRTC || in.Target || len(in.Exits) > 0) {
		return errors.New("Network evidence requires explicit network checks.")
	}
	if len(in.Exits) > 8 {
		return errors.New("Too many browser observations.")
	}
	seen := map[string]bool{}
	for i := range in.Exits {
		x := &in.Exits[i]
		if x.Intel != nil || x.Path != "browser" && x.Path != "webrtc" || x.Path == "webrtc" && !in.WebRTC || x.Family != "ipv4" && x.Family != "ipv6" {
			return errors.New("Invalid browser observation.")
		}
		if x.IP != "" {
			if !publicIP(x.IP) || familyOf(x.IP) != x.Family || x.Error != "" {
				return errors.New("Invalid public egress address.")
			}
		}
		x.IP = canonicalIP(x.IP)
		key := x.Path + ":" + x.Family
		if x.Path == "webrtc" {
			key += "/" + x.IP
		}
		if seen[key] {
			return errors.New("Duplicate browser observation.")
		}
		seen[key] = true
		if x.Error != "" {
			x.Error = "Browser probe unavailable or timed out."
		}
	}
	return nil
}
func (s *Server) run(w http.ResponseWriter, r *http.Request) {
	var in RunInput
	if !readJSON(w, r, &in) {
		return
	}
	if err := validateRun(&in); err != nil {
		fail(w, err)
		return
	}
	p, err := s.profile(in.ProfileID)
	if err != nil {
		fail(w, err)
		return
	}
	if p.Revision != in.Revision {
		fail(w, ErrConflict)
		return
	}
	select {
	case s.runGate <- struct{}{}:
		defer func() { <-s.runGate }()
	default:
		fail(w, errors.New("A diagnostic run is already in progress."))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	opts := s.opts
	opts.TargetOrigin = p.Origin
	opts.IPSettings = s.store.Snapshot().IPSettings
	if in.Intelligence && in.IPSettingsRevision != opts.IPSettings.Revision {
		fail(w, errors.New("IP provider settings changed. Reload settings and enable IP intelligence again."))
		return
	}
	e := CollectNetwork(ctx, in, opts)
	if ctx.Err() != nil {
		fail(w, errors.New("Diagnostic run canceled or timed out. Partial results were not saved."))
		return
	}
	fresh, err := s.profile(p.ID)
	if err != nil || fresh.Revision != p.Revision {
		fail(w, ErrConflict)
		return
	}
	report := Score(e, p.Policy, time.Now())
	report.ProfileID = p.ID
	report.ProfileRevision = p.Revision
	s.mu.Lock()
	s.current[p.ID] = report
	s.mu.Unlock()
	writeJSON(w, 200, report)
}
func (s *Server) findReport(profileID, reportID string) (RedactedReport, error) {
	if _, err := s.profile(profileID); err != nil {
		return RedactedReport{}, err
	}
	s.mu.Lock()
	v, ok := s.current[profileID]
	s.mu.Unlock()
	if ok && v.ID == reportID {
		return Redact(v), nil
	}
	for _, v := range s.store.Snapshot().Reports {
		if v.ID == reportID && v.ProfileID == profileID {
			return v, nil
		}
	}
	return RedactedReport{}, errors.New("Selected report was replaced or does not belong to this service. Select a report again.")
}
func (s *Server) modelClient(settings ModelSettings) (*ModelClient, error) {
	c, err := NewModelClient(settings, s.opts.ModelToken)
	if err != nil {
		return nil, err
	}
	if settings.Provider == "claude-cli" || settings.Provider == "codex-cli" {
		return c, nil
	}
	u, _ := url.Parse(settings.Endpoint)
	_, ownPort, _ := net.SplitHostPort(s.opts.Host)
	port := u.Port()
	if port == "" {
		if u.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	if port == ownPort {
		return nil, errors.New("The model endpoint cannot use this application's port.")
	}
	return c, nil
}
func (s *Server) saveSettings(w http.ResponseWriter, r *http.Request) {
	var settings ModelSettings
	if !readJSON(w, r, &settings) {
		return
	}
	if err := normalizeModelSettings(&settings); err != nil {
		fail(w, err)
		return
	}
	c, err := s.modelClient(settings)
	if err != nil {
		fail(w, err)
		return
	}
	c.CloseIdleConnections()
	if err = s.store.SaveSettings(settings); err != nil {
		fail(w, err)
		return
	}
	s.mu.Lock()
	s.conversations = map[string]*conversation{}
	s.mu.Unlock()
	writeJSON(w, 200, settings)
}
func (s *Server) previewContext(w http.ResponseWriter, r *http.Request) {
	var v struct {
		Mode      string `json:"mode"`
		ProfileID string `json:"profileId"`
		ReportID  string `json:"reportId"`
		Notes     string `json:"notes"`
		Stage     string `json:"stage"`
		ErrorText string `json:"errorText"`
	}
	if !readJSON(w, r, &v) {
		return
	}
	issue, err := issueContext(v.Stage, v.ErrorText)
	if err != nil {
		fail(w, err)
		return
	}
	var report RedactedReport
	text := "No diagnostic report is attached. No stored service profiles or notes were selected."
	switch v.Mode {
	case "direct":
		if v.ProfileID != "" || v.ReportID != "" || v.Notes != "" {
			fail(w, errors.New("Direct chat cannot attach stored service data. Choose report mode to review a report."))
			return
		}
	case "", "report": // Keep existing report clients compatible.
		report, err = s.findReport(v.ProfileID, v.ReportID)
		if err == nil {
			text, err = reportContext(report, v.Notes)
		}
		if err != nil {
			fail(w, err)
			return
		}
	default:
		fail(w, errors.New("Choose direct chat or a selected report."))
		return
	}
	text += issue
	if len(text) > 24<<10 {
		fail(w, errors.New("Selected context exceeds 24 KiB; shorten the error or optional notes."))
		return
	}
	settings := s.store.Snapshot().Settings
	c := &conversation{ID: newID(), ProfileID: v.ProfileID, ReportID: v.ReportID, Context: text, Settings: settings, Created: time.Now(), Messages: []Message{}}
	s.mu.Lock()
	for id, old := range s.conversations {
		if time.Since(old.Created) > 30*time.Minute || old.ProfileID == v.ProfileID {
			delete(s.conversations, id)
		}
	}
	if len(s.conversations) >= 20 {
		s.mu.Unlock()
		fail(w, errors.New("Too many open contexts. Reload the application to start a new session."))
		return
	}
	s.conversations[c.ID] = c
	s.mu.Unlock()
	delivery := "direct"
	if settings.Provider == "codex-cli" {
		delivery = "manual"
	}
	writeJSON(w, 200, map[string]any{"delivery": delivery, "id": c.ID, "context": text, "systemPrompt": assistantInstructions, "settings": settings, "reportId": report.ID, "checkedAt": report.CreatedAt})
}
func (s *Server) chat(w http.ResponseWriter, r *http.Request) {
	var v struct {
		ContextID string `json:"contextId"`
		Message   string `json:"message"`
	}
	if !readJSON(w, r, &v) {
		return
	}
	if strings.TrimSpace(v.Message) == "" || len(v.Message) > 8192 {
		fail(w, errors.New("Enter a question of up to 8 KiB."))
		return
	}
	select {
	case s.chatGate <- struct{}{}:
		defer func() { <-s.chatGate }()
	default:
		fail(w, errors.New("A model response is already in progress."))
		return
	}
	s.mu.Lock()
	c, ok := s.conversations[v.ContextID]
	if !ok || time.Since(c.Created) > 30*time.Minute {
		s.mu.Unlock()
		fail(w, errors.New("Conversation expired. Start a new conversation or preview the report again."))
		return
	}
	snapshot := *c
	snapshot.Messages = append([]Message{}, c.Messages...)
	s.mu.Unlock()
	settings := s.store.Snapshot().Settings
	if settings != snapshot.Settings {
		fail(w, errors.New("Model settings changed. Preview context again."))
		return
	}
	if snapshot.ReportID != "" {
		if _, err := s.findReport(snapshot.ProfileID, snapshot.ReportID); err != nil {
			fail(w, err)
			return
		}
	}
	if len(snapshot.Messages)+2 > 20 {
		fail(w, errors.New("Conversation limit reached. Start a fresh context."))
		return
	}
	messages := []Message{{Role: "system", Content: assistantInstructions}, {Role: "user", Content: snapshot.Context}}
	messages = append(messages, snapshot.Messages...)
	messages = append(messages, Message{Role: "user", Content: v.Message})
	client, err := s.modelClient(settings)
	if err != nil {
		fail(w, err)
		return
	}
	text, err := client.Chat(r.Context(), settings.Model, messages)
	if err != nil {
		fail(w, err)
		return
	}
	if r.Context().Err() != nil {
		return
	}
	// Reject results after profile deletion, source replacement, or a settings/context change.
	if snapshot.ReportID != "" {
		if _, err = s.findReport(snapshot.ProfileID, snapshot.ReportID); err != nil {
			fail(w, err)
			return
		}
	}
	s.mu.Lock()
	current, ok := s.conversations[v.ContextID]
	if !ok || current != c {
		s.mu.Unlock()
		fail(w, errors.New("Chat context changed; the stale answer was discarded."))
		return
	}
	current.Messages = append(current.Messages, Message{Role: "user", Content: v.Message}, Message{Role: "assistant", Content: text})
	s.mu.Unlock()
	writeJSON(w, 200, map[string]string{"reply": text})
}
func (s *Server) exportProfile(w http.ResponseWriter, r *http.Request) {
	p, err := s.profile(r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	if r.URL.Query().Get("includeNotes") != "true" {
		p.Notes = ""
		for i := range p.Issues {
			p.Issues[i].Notes = ""
			p.Issues[i].Error = ""
		}
	}
	reports := []RedactedReport{}
	for _, v := range s.store.Snapshot().Reports {
		if v.ProfileID == p.ID {
			reports = append(reports, v)
		}
	}
	w.Header().Set("Content-Disposition", `attachment; filename="service-profile.json"`)
	writeJSON(w, 200, map[string]any{"profile": p, "reports": reports, "notice": "User-authored text may contain private information. Review before sharing."})
}
