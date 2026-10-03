package app

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"
)

// Explicit view prevents private credentials from joining future response fields.
type careerView struct {
	Revision      int             `json:"revision"`
	Companies     []CareerCompany `json:"companies"`
	Student       StudentProfile  `json:"student"`
	Jobs          []CareerJob     `json:"jobs"`
	Updates       []CompanyUpdate `json:"updates"`
	Usage         UsageSnapshot   `json:"usage"`
	Advice        []CareerAdvice  `json:"advice"`
	KeyConfigured bool            `json:"keyConfigured"`
}

func careerPublic(c CareerState) careerView {
	return careerView{c.Revision, c.Companies, c.Student, c.Jobs, c.Updates, c.Usage, c.Advice, c.OpenRouterKey != ""}
}
func (s *Server) careerRevision(ctx context.Context, revision int) (CareerState, error) {
	if ctx.Err() != nil {
		return CareerState{}, errors.New("Career operation canceled; nothing was saved.")
	}
	c := s.store.Snapshot().Career
	if c == nil || c.Revision != revision {
		return CareerState{}, ErrConflict
	}
	return *c, nil
}
func (s *Server) saveCareer(w http.ResponseWriter, r *http.Request, c CareerState, revision int) {
	s.saveCareerSettings(w, r, c, revision, nil)
}
func (s *Server) saveCareerSettings(w http.ResponseWriter, r *http.Request, c CareerState, revision int, settings *ModelSettings) {
	if _, err := s.careerRevision(r.Context(), revision); err != nil {
		fail(w, err)
		return
	}
	next, err := s.store.saveCareer(r.Context(), c, revision, settings)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, careerPublic(next))
}
func (s *Server) registerCareerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/career", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, careerPublic(*s.store.Snapshot().Career))
	})
	mux.HandleFunc("POST /api/career/companies", s.saveCareerCompany)
	mux.HandleFunc("DELETE /api/career/companies/{id}", s.deleteCareerCompany)
	mux.HandleFunc("PUT /api/career/student", s.saveCareerStudent)
	mux.HandleFunc("POST /api/career/key", s.saveCareerKey)
	mux.HandleFunc("POST /api/career/refresh", s.refreshCareer)
	mux.HandleFunc("POST /api/career/paste", s.pasteCareer)
	mux.HandleFunc("POST /api/career/usage", s.refreshCareerUsage)
	mux.HandleFunc("POST /api/career/preview", s.previewCareer)
	mux.HandleFunc("POST /api/career/analyze", s.analyzeCareer)
	mux.HandleFunc("POST /api/career/advice", s.saveCareerAdvice)
	mux.HandleFunc("DELETE /api/career/advice/{id}", s.deleteCareerAdvice)
}
func (s *Server) saveCareerCompany(w http.ResponseWriter, r *http.Request) {
	var v struct {
		Revision int           `json:"revision"`
		Company  CareerCompany `json:"company"`
	}
	if !readJSON(w, r, &v) {
		return
	}
	c, err := s.careerRevision(r.Context(), v.Revision)
	if err != nil {
		fail(w, err)
		return
	}
	i := slices.IndexFunc(c.Companies, func(co CareerCompany) bool { return co.ID == v.Company.ID })
	if v.Company.ID != "" && i < 0 {
		fail(w, errors.New("Company not found."))
		return
	}
	// Refresh metadata is server-owned. Editing source URLs drops old-source freshness.
	v.Company.Refreshes = []CareerRefresh{}
	if i < 0 {
		v.Company.ID = newID()
		c.Companies = append(c.Companies, v.Company)
	} else {
		v.Company.Refreshes = c.Companies[i].Refreshes
		c.Companies[i] = v.Company
	}
	co := &c.Companies[len(c.Companies)-1]
	if i >= 0 {
		co = &c.Companies[i]
	}
	if co.CareersURL != "" {
		source, e := resolveCareerSource(co.CareersURL, "jobs")
		if e != nil {
			fail(w, e)
			return
		}
		co.CareersURL = source.URL
	}
	for j, raw := range co.NewsURLs {
		source, e := resolveCareerSource(raw, "updates")
		if e != nil {
			fail(w, e)
			return
		}
		co.NewsURLs[j] = source.URL
	}
	normalizeCareer(&c)
	s.saveCareer(w, r, c, v.Revision)
}
func (s *Server) deleteCareerCompany(w http.ResponseWriter, r *http.Request) {
	var v struct {
		Revision int `json:"revision"`
	}
	if !readJSON(w, r, &v) {
		return
	}
	c, err := s.careerRevision(r.Context(), v.Revision)
	if err != nil {
		fail(w, err)
		return
	}
	id := r.PathValue("id")
	if !slices.ContainsFunc(c.Companies, func(co CareerCompany) bool { return co.ID == id }) {
		fail(w, errors.New("Company not found."))
		return
	}
	c.Companies = slices.DeleteFunc(c.Companies, func(co CareerCompany) bool { return co.ID == id })
	c.Jobs = slices.DeleteFunc(c.Jobs, func(j CareerJob) bool { return j.CompanyID == id })
	c.Updates = slices.DeleteFunc(c.Updates, func(u CompanyUpdate) bool { return u.CompanyID == id })
	c.Advice = slices.DeleteFunc(c.Advice, func(a CareerAdvice) bool {
		return slices.ContainsFunc(a.Sources, func(source CareerCitation) bool { return source.CompanyID == id })
	})
	s.saveCareer(w, r, c, v.Revision)
}
func (s *Server) saveCareerStudent(w http.ResponseWriter, r *http.Request) {
	var v struct {
		Revision int            `json:"revision"`
		Student  StudentProfile `json:"student"`
	}
	if !readJSON(w, r, &v) {
		return
	}
	c, err := s.careerRevision(r.Context(), v.Revision)
	if err != nil {
		fail(w, err)
		return
	}
	c.Student = v.Student
	s.saveCareer(w, r, c, v.Revision)
}
func (s *Server) saveCareerKey(w http.ResponseWriter, r *http.Request) {
	var v struct {
		Revision int    `json:"revision"`
		Key      string `json:"key"`
		ClearKey bool   `json:"clearKey"`
	}
	if !readJSON(w, r, &v) {
		return
	}
	c, err := s.careerRevision(r.Context(), v.Revision)
	if err != nil {
		fail(w, err)
		return
	}
	if v.ClearKey && strings.TrimSpace(v.Key) != "" {
		fail(w, errors.New("Choose a new key or clear the saved key."))
		return
	}
	if v.ClearKey {
		c.OpenRouterKey = ""
	} else if strings.TrimSpace(v.Key) != "" {
		c.OpenRouterKey = strings.TrimSpace(v.Key)
	}
	s.saveCareer(w, r, c, v.Revision)
}
func (s *Server) careerSourceHTTP(source CareerSource) (*http.Client, error) {
	client, err := careerSourceClient(s.opts, source)
	if err != nil {
		return nil, err
	}
	if s.careerTransport != nil {
		client.Transport = s.careerTransport
	}
	return client, nil
}
func setCareerRefresh(c *CareerState, index int, source CareerSource, checked string, complete bool, err error) {
	rows := c.Companies[index].Refreshes
	i := slices.IndexFunc(rows, func(row CareerRefresh) bool { return row.Kind == source.Kind && row.SourceURL == source.URL })
	next := CareerRefresh{Kind: source.Kind, SourceURL: source.URL, CheckedAt: checked, Complete: complete}
	if i >= 0 {
		next.LastSuccess = rows[i].LastSuccess
	}
	if err != nil {
		next.Error, _ = boundedText(err.Error(), 512)
		next.Complete = false
	} else {
		next.LastSuccess = checked
	}
	if i >= 0 {
		rows[i] = next
	} else {
		rows = append(rows, next)
	}
	c.Companies[index].Refreshes = rows
}
func mergeCareerUpdates(c CareerState, companyID string, updates []CompanyUpdate) (CareerState, error) {
	for _, u := range updates {
		u.CompanyID = companyID
		i := slices.IndexFunc(c.Updates, func(old CompanyUpdate) bool {
			oldURL, _ := normalizeCareerURL(old.URL)
			newURL, _ := normalizeCareerURL(u.URL)
			return old.CompanyID == companyID && old.Kind != "pasted" && u.Kind != "pasted" && (oldURL != "" && oldURL == newURL || old.SourceURL == u.SourceURL && old.SourceID != "" && old.SourceID == u.SourceID)
		})
		if i >= 0 {
			u.ID = c.Updates[i].ID
			c.Updates[i] = u
		} else {
			u.ID = newID()
			c.Updates = append(c.Updates, u)
		}
	}
	if err := validateCareer(c); err != nil {
		return CareerState{}, err
	}
	return c, nil
}
func (s *Server) refreshCareer(w http.ResponseWriter, r *http.Request) {
	var v struct {
		Revision  int    `json:"revision"`
		CompanyID string `json:"companyId"`
		Kind      string `json:"kind"`
	}
	if !readJSON(w, r, &v) {
		return
	}
	c, err := s.careerRevision(r.Context(), v.Revision)
	if err != nil {
		fail(w, err)
		return
	}
	i := slices.IndexFunc(c.Companies, func(co CareerCompany) bool { return co.ID == v.CompanyID })
	if i < 0 || !careerEnum(v.Kind, "jobs", "updates") {
		fail(w, errors.New("Choose a saved company and jobs or updates."))
		return
	}
	urls := c.Companies[i].NewsURLs
	if v.Kind == "jobs" {
		urls = []string{c.Companies[i].CareersURL}
	}
	sources := []CareerSource{}
	for _, raw := range urls {
		source, e := resolveCareerSource(raw, v.Kind)
		if e != nil {
			fail(w, e)
			return
		}
		sources = append(sources, source)
	}
	if len(sources) == 0 {
		fail(w, errors.New("Save a source URL before refreshing."))
		return
	}
	select {
	case s.careerGate <- struct{}{}:
		defer func() { <-s.careerGate }()
	default:
		fail(w, errors.New("A career source refresh is already in progress."))
		return
	}
	for _, source := range sources {
		if _, err = s.careerRevision(r.Context(), v.Revision); err != nil {
			fail(w, err)
			return
		}
		client, fetchErr := s.careerSourceHTTP(source)
		complete := false
		checked := timestamp()
		if fetchErr == nil {
			if v.Kind == "jobs" {
				var batch CareerJobBatch
				batch, fetchErr = fetchCareerJobs(r.Context(), client, source)
				if fetchErr == nil {
					c, err = mergeCareerJobs(c, v.CompanyID, batch)
					complete = batch.Complete
					checked = batch.CheckedAt
				}
			} else {
				var updates []CompanyUpdate
				updates, fetchErr = fetchCareerUpdates(r.Context(), client, source)
				if fetchErr == nil {
					c, err = mergeCareerUpdates(c, v.CompanyID, updates)
					// Update adapters return bounded samples or page snapshots, not proven complete boards.
					complete = false
				}
			}
			client.CloseIdleConnections()
		}
		if _, e := s.careerRevision(r.Context(), v.Revision); e != nil {
			fail(w, e)
			return
		}
		if err != nil {
			fail(w, err)
			return
		} // Capacity/storage failures never persist a partial refresh.
		setCareerRefresh(&c, i, source, checked, complete, fetchErr)
	}
	s.saveCareer(w, r, c, v.Revision)
}
func (s *Server) pasteCareer(w http.ResponseWriter, r *http.Request) {
	var v struct {
		Revision  int    `json:"revision"`
		CompanyID string `json:"companyId"`
		Kind      string `json:"kind"`
		Title     string `json:"title"`
		URL       string `json:"url"`
		Text      string `json:"text"`
	}
	if !readJSON(w, r, &v) {
		return
	}
	c, err := s.careerRevision(r.Context(), v.Revision)
	if err != nil {
		fail(w, err)
		return
	}
	if !slices.ContainsFunc(c.Companies, func(co CareerCompany) bool { return co.ID == v.CompanyID }) || !careerEnum(v.Kind, "jobs", "updates") || strings.TrimSpace(v.Text) == "" || !careerText(6144, v.Text) || !careerText(512, v.Title) || strings.TrimSpace(v.Title) == "" {
		fail(w, errors.New("Choose a saved company, title, and pasted text of at most 6 KiB."))
		return
	}
	if v.URL != "" {
		v.URL, err = normalizeCareerURL(v.URL)
		if err != nil {
			fail(w, err)
			return
		}
	}
	if v.Kind == "jobs" {
		level, basis := careerLevel(v.Title, "", v.Text)
		c.Jobs = append(c.Jobs, CareerJob{ID: newID(), CompanyID: v.CompanyID, Provider: "paste", URL: v.URL, Title: v.Title, Text: v.Text, Level: level, LevelBasis: basis, DateBasis: "unknown", ListingState: "unknown", FetchedAt: timestamp()})
	} else {
		c.Updates = append(c.Updates, CompanyUpdate{ID: newID(), CompanyID: v.CompanyID, URL: v.URL, Title: v.Title, Text: v.Text, Kind: "pasted", FetchedAt: timestamp()})
	}
	s.saveCareer(w, r, c, v.Revision)
}
func (s *Server) refreshCareerUsage(w http.ResponseWriter, r *http.Request) {
	var v struct {
		Revision int `json:"revision"`
	}
	if !readJSON(w, r, &v) {
		return
	}
	c, err := s.careerRevision(r.Context(), v.Revision)
	if err != nil {
		fail(w, err)
		return
	}
	if c.OpenRouterKey == "" {
		fail(w, errors.New("Configure an OpenRouter key before fetching usage."))
		return
	}
	select {
	case s.careerGate <- struct{}{}:
		defer func() { <-s.careerGate }()
	default:
		fail(w, errors.New("A career source refresh is already in progress."))
		return
	}
	client, err := networkClient(s.opts)
	if err != nil {
		fail(w, err)
		return
	}
	defer client.CloseIdleConnections()
	if s.careerTransport != nil {
		client.Transport = s.careerTransport
	}
	c.Usage, err = fetchCareerUsage(r.Context(), client, c.OpenRouterKey, time.Now())
	if _, e := s.careerRevision(r.Context(), v.Revision); e != nil {
		fail(w, e)
		return
	}
	if err != nil {
		fail(w, err)
		return
	}
	s.saveCareer(w, r, c, v.Revision)
}
func (s *Server) previewCareer(w http.ResponseWriter, r *http.Request) {
	var v struct {
		Revision  int             `json:"revision"`
		Selection CareerSelection `json:"selection"`
	}
	if !readJSON(w, r, &v) {
		return
	}
	c, err := s.careerRevision(r.Context(), v.Revision)
	if err != nil {
		fail(w, err)
		return
	}
	preview, err := careerMessages(c, v.Selection)
	if err != nil {
		fail(w, err)
		return
	}
	settings := s.store.Snapshot().Settings
	delivery := "direct"
	if settings.Provider == "codex-cli" {
		delivery = "manual"
	}
	writeJSON(w, 200, struct {
		CareerPreview
		Settings ModelSettings `json:"settings"`
		Delivery string        `json:"delivery"`
	}{preview, settings, delivery})
}
func (s *Server) analyzeCareer(w http.ResponseWriter, r *http.Request) {
	var v struct {
		Revision    int             `json:"revision"`
		Selection   CareerSelection `json:"selection"`
		PreviewHash string          `json:"previewHash"`
		Settings    ModelSettings   `json:"settings"`
	}
	if !readJSON(w, r, &v) {
		return
	}
	c, err := s.careerRevision(r.Context(), v.Revision)
	if err != nil {
		fail(w, err)
		return
	}
	preview, err := careerMessages(c, v.Selection)
	if err != nil {
		fail(w, err)
		return
	}
	if preview.Hash != v.PreviewHash || s.store.Snapshot().Settings != v.Settings {
		fail(w, ErrConflict)
		return
	}
	if v.Settings.Provider == "codex-cli" {
		fail(w, errors.New("Codex delivery is manual. Copy the reviewed context, then explicitly save your edited note."))
		return
	}
	select {
	case s.chatGate <- struct{}{}:
		defer func() { <-s.chatGate }()
	default:
		fail(w, errors.New("A model response is already in progress."))
		return
	}
	client, err := s.modelClient(v.Settings)
	if err != nil {
		fail(w, err)
		return
	}
	defer client.CloseIdleConnections()
	if _, err = s.careerRevision(r.Context(), v.Revision); err != nil {
		fail(w, err)
		return
	}
	if s.store.Snapshot().Settings != v.Settings {
		fail(w, ErrConflict)
		return
	}
	reply, callErr := client.Chat(r.Context(), v.Settings.Model, preview.Messages)
	if _, err = s.careerRevision(r.Context(), v.Revision); err != nil {
		fail(w, err)
		return
	}
	if s.store.Snapshot().Settings != v.Settings {
		fail(w, ErrConflict)
		return
	}
	if callErr != nil {
		fail(w, callErr)
		return
	}
	writeJSON(w, 200, map[string]any{"revision": v.Revision, "reply": reply, "sources": preview.Sources, "settings": v.Settings, "previewHash": preview.Hash})
}
func (s *Server) saveCareerAdvice(w http.ResponseWriter, r *http.Request) {
	var v struct {
		Revision  int             `json:"revision"`
		Text      string          `json:"text"`
		Selection CareerSelection `json:"selection"`
		Settings  ModelSettings   `json:"settings"`
	}
	if !readJSON(w, r, &v) {
		return
	}
	c, err := s.careerRevision(r.Context(), v.Revision)
	if err != nil {
		fail(w, err)
		return
	}
	if s.store.Snapshot().Settings != v.Settings {
		fail(w, ErrConflict)
		return
	}
	preview, err := careerMessages(c, v.Selection)
	if err != nil {
		fail(w, err)
		return
	}
	c.Advice = append(c.Advice, CareerAdvice{ID: newID(), Text: v.Text, CreatedAt: timestamp(), Provider: v.Settings.Provider, Model: v.Settings.Model, Sources: preview.Sources})
	s.saveCareerSettings(w, r, c, v.Revision, &v.Settings)
}
func (s *Server) deleteCareerAdvice(w http.ResponseWriter, r *http.Request) {
	var v struct {
		Revision int `json:"revision"`
	}
	if !readJSON(w, r, &v) {
		return
	}
	c, err := s.careerRevision(r.Context(), v.Revision)
	if err != nil {
		fail(w, err)
		return
	}
	id := r.PathValue("id")
	if !slices.ContainsFunc(c.Advice, func(a CareerAdvice) bool { return a.ID == id }) {
		fail(w, errors.New("Saved note not found."))
		return
	}
	c.Advice = slices.DeleteFunc(c.Advice, func(a CareerAdvice) bool { return a.ID == id })
	s.saveCareer(w, r, c, v.Revision)
}
