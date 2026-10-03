package app

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// CareerState is private local data. API handlers must return a dedicated view.
type CareerState struct {
	Revision      int             `json:"revision"`
	Companies     []CareerCompany `json:"companies"`
	Student       StudentProfile  `json:"student"`
	Jobs          []CareerJob     `json:"jobs"`
	Updates       []CompanyUpdate `json:"updates"`
	Usage         UsageSnapshot   `json:"usage"`
	Advice        []CareerAdvice  `json:"advice"`
	OpenRouterKey string          `json:"openRouterKey"`
}
type CareerCompany struct {
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	Website       string          `json:"website"`
	CareersURL    string          `json:"careersUrl"`
	NewsURLs      []string        `json:"newsUrls"`
	ModelPrefixes []string        `json:"modelPrefixes"`
	Refreshes     []CareerRefresh `json:"refreshes"`
}
type CareerRefresh struct {
	Kind        string `json:"kind"`
	SourceURL   string `json:"sourceUrl"`
	CheckedAt   string `json:"checkedAt"`
	LastSuccess string `json:"lastSuccess"`
	Error       string `json:"error"`
	Complete    bool   `json:"complete"`
}
type StudentProfile struct {
	Stage     string `json:"stage"`
	Skills    string `json:"skills"`
	Projects  string `json:"projects"`
	Roles     string `json:"roles"`
	Locations string `json:"locations"`
}
type CareerJob struct {
	ID             string `json:"id"`
	CompanyID      string `json:"companyId"`
	Provider       string `json:"provider"`
	ProviderID     string `json:"providerId"`
	SourceURL      string `json:"sourceUrl"`
	URL            string `json:"url"`
	Title          string `json:"title"`
	Location       string `json:"location"`
	Workplace      string `json:"workplace"`
	EmploymentType string `json:"employmentType"`
	Text           string `json:"text"`
	Level          string `json:"level"`
	LevelBasis     string `json:"levelBasis"`
	PublishedAt    string `json:"publishedAt"`
	UpdatedAt      string `json:"updatedAt"`
	DateBasis      string `json:"dateBasis"`
	FetchedAt      string `json:"fetchedAt"`
	ListingState   string `json:"listingState"`
	Truncated      bool   `json:"truncated"`
}
type CompanyUpdate struct {
	ID          string `json:"id"`
	CompanyID   string `json:"companyId"`
	SourceID    string `json:"sourceId"`
	SourceURL   string `json:"sourceUrl"`
	URL         string `json:"url"`
	Title       string `json:"title"`
	Text        string `json:"text"`
	Kind        string `json:"kind"`
	PublishedAt string `json:"publishedAt"`
	FetchedAt   string `json:"fetchedAt"`
	Truncated   bool   `json:"truncated"`
}
type UsageRow struct {
	Date        string `json:"date"`
	Model       string `json:"model"`
	TotalTokens string `json:"totalTokens"`
}
type UsageSnapshot struct {
	Rows      []UsageRow `json:"rows"`
	AsOf      string     `json:"asOf"`
	Version   string     `json:"version"`
	StartDate string     `json:"startDate"`
	EndDate   string     `json:"endDate"`
	FetchedAt string     `json:"fetchedAt"`
	SourceURL string     `json:"sourceUrl"`
}
type CareerCitation struct {
	ID          string `json:"id"`
	CompanyID   string `json:"companyId"`
	Title       string `json:"title"`
	URL         string `json:"url"`
	PublishedAt string `json:"publishedAt"`
	FetchedAt   string `json:"fetchedAt"`
}
type CareerAdvice struct {
	ID        string           `json:"id"`
	Text      string           `json:"text"`
	CreatedAt string           `json:"createdAt"`
	Provider  string           `json:"provider"`
	Model     string           `json:"model"`
	Sources   []CareerCitation `json:"sources"`
}

func newCareerState() *CareerState {
	return &CareerState{Revision: 1, Companies: []CareerCompany{}, Jobs: []CareerJob{}, Updates: []CompanyUpdate{}, Advice: []CareerAdvice{}, Usage: UsageSnapshot{Rows: []UsageRow{}}}
}

// normalizeCareerURL validates stored links. Fetch adapters separately restrict
// arbitrary queries and recognize provider parameters; this does not authorize a fetch.
func normalizeCareerURL(raw string) (string, error) {
	if !utf8.ValidString(raw) || len(raw) > 2048 || strings.ContainsFunc(raw, unicode.IsControl) || strings.Contains(raw, "#") {
		return "", errors.New("Career links must be public HTTPS URLs without credentials or fragments, at most 2 KiB.")
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.Opaque != "" || u.Hostname() == "" || u.User != nil || (u.Port() != "" && u.Port() != "443") || strings.ContainsAny(u.Host, "\\% ") || strings.HasSuffix(u.Host, ":") {
		return "", errors.New("Career links must use public HTTPS port 443 without credentials or fragments.")
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if ip, err := netip.ParseAddr(host); err == nil {
		if ip.Zone() != "" || !publicIP(host) {
			return "", errors.New("Private or reserved career destinations are not allowed.")
		}
		host = ip.Unmap().String()
	} else {
		if len(host) > 253 || !strings.Contains(host, ".") || host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") {
			return "", errors.New("Use a public career destination.")
		}
		for _, label := range strings.Split(host, ".") {
			if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' || strings.ContainsFunc(label, func(r rune) bool { return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') }) {
				return "", errors.New("Invalid career destination.")
			}
		}
	}
	u.Host = host
	if net.ParseIP(host) != nil && strings.Contains(host, ":") {
		u.Host = "[" + host + "]"
	}
	if u.Path == "" {
		u.Path = "/"
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return "", errors.New("Invalid career link query.")
	}
	for key := range query {
		low := strings.ToLower(key)
		if strings.Contains(low, "token") || strings.Contains(low, "secret") || strings.Contains(low, "password") || strings.Contains(low, "api_key") || strings.Contains(low, "apikey") || low == "key" || low == "authorization" {
			return "", errors.New("Career links must not contain credentials.")
		}
	}
	u.RawQuery = query.Encode()
	return u.String(), nil
}
func careerText(limit int, fields ...string) bool {
	for _, v := range fields {
		if !utf8.ValidString(v) || len(v) > limit {
			return false
		}
	}
	return true
}
func careerDate(v string, required, day bool) bool {
	if v == "" {
		return !required
	}
	if day {
		if _, err := time.Parse("2006-01-02", v); err == nil {
			return true
		}
	}
	_, err := time.Parse(time.RFC3339, v)
	return err == nil
}
func careerLink(v string, required bool) bool {
	if v == "" {
		return !required
	}
	_, err := normalizeCareerURL(v)
	return err == nil
}
func careerEnum(v string, options ...string) bool { return slices.Contains(options, v) }

func validateCareer(c CareerState) error {
	bad := errors.New("Invalid Career Radar data or capacity exceeded.")
	if c.Revision < 1 || c.Companies == nil || c.Jobs == nil || c.Updates == nil || c.Advice == nil ||
		c.Usage.Rows == nil || len(c.Companies) > 50 || len(c.Jobs) > 1000 || len(c.Updates) > 300 || len(c.Advice) > 50 {
		return bad
	}
	if !careerText(8192, c.Student.Stage, c.Student.Skills, c.Student.Projects, c.Student.Roles, c.Student.Locations) ||
		len(c.Student.Stage)+len(c.Student.Skills)+len(c.Student.Projects)+len(c.Student.Roles)+len(c.Student.Locations) > 8192 ||
		!careerText(512, c.OpenRouterKey) || strings.ContainsFunc(c.OpenRouterKey, unicode.IsControl) {
		return bad
	}
	companies := map[string]bool{}
	for _, co := range c.Companies {
		if !idPattern.MatchString(co.ID) || companies[co.ID] || !utf8.ValidString(co.Name) ||
			strings.TrimSpace(co.Name) == "" || utf8.RuneCountInString(co.Name) > 160 || !careerLink(co.Website, false) ||
			!careerLink(co.CareersURL, false) || co.NewsURLs == nil || co.ModelPrefixes == nil || co.Refreshes == nil ||
			len(co.NewsURLs) > 3 || len(co.Refreshes) > 4 || len(co.ModelPrefixes) > 100 {
			return bad
		}
		companies[co.ID] = true
		sources := map[string]string{}
		if co.CareersURL != "" {
			v, _ := normalizeCareerURL(co.CareersURL)
			sources[v] = "jobs"
		}
		for _, v := range co.NewsURLs {
			u, err := normalizeCareerURL(v)
			if err != nil || sources[u] != "" {
				return bad
			}
			sources[u] = "updates"
		}
		prefixes := map[string]bool{}
		for _, v := range co.ModelPrefixes {
			if !careerText(160, v) || strings.TrimSpace(v) == "" || prefixes[v] {
				return bad
			}
			prefixes[v] = true
		}
		refreshes := map[string]bool{}
		for _, r := range co.Refreshes {
			u, err := normalizeCareerURL(r.SourceURL)
			if err != nil || sources[u] != r.Kind || !careerEnum(r.Kind, "jobs", "updates") || refreshes[u] ||
				!careerDate(r.CheckedAt, true, false) || !careerDate(r.LastSuccess, false, false) || !careerText(6144, r.Error) ||
				(r.Error != "" && r.Complete) {
				return bad
			}
			refreshes[u] = true
		}
	}
	identities := map[string]bool{}
	for _, j := range c.Jobs {
		if !idPattern.MatchString(j.ID) || identities[j.ID] || !companies[j.CompanyID] ||
			!careerEnum(j.Provider, "greenhouse", "ashby", "lever", "lever-eu", "custom", "paste") ||
			!careerText(512, j.ProviderID) || !careerLink(j.SourceURL, j.Provider != "paste") || !careerLink(j.URL, false) ||
			!careerText(6144, j.Text) || !careerText(512, j.Title, j.Location, j.Workplace, j.EmploymentType) ||
			strings.TrimSpace(j.Title) == "" || !careerEnum(j.Level, "internship", "new-graduate", "entry-level", "unknown") ||
			!careerEnum(j.LevelBasis, "title-inferred", "posting-explicit", "unknown") ||
			!careerEnum(j.DateBasis, "published", "last-published", "updated", "unknown") ||
			!careerEnum(j.ListingState, "listed", "no-longer-listed", "unknown") || !careerDate(j.PublishedAt, false, true) ||
			!careerDate(j.UpdatedAt, false, true) || !careerDate(j.FetchedAt, true, false) {
			return bad
		}
		identities[j.ID] = true
	}
	identities = map[string]bool{}
	for _, u := range c.Updates {
		if !idPattern.MatchString(u.ID) || identities[u.ID] || !companies[u.CompanyID] ||
			!careerText(512, u.SourceID, u.Title) || strings.TrimSpace(u.Title) == "" || !careerText(6144, u.Text) ||
			!careerEnum(u.Kind, "feed-item", "page-snapshot", "article", "pasted") ||
			!careerLink(u.SourceURL, u.Kind != "pasted") || !careerLink(u.URL, false) ||
			!careerDate(u.PublishedAt, false, true) || !careerDate(u.FetchedAt, true, false) {
			return bad
		}
		identities[u.ID] = true
	}
	if err := validateCareerUsage(c.Usage); err != nil {
		return err
	}
	identities = map[string]bool{}
	for _, a := range c.Advice {
		if !idPattern.MatchString(a.ID) || identities[a.ID] || !careerText(16384, a.Text) ||
			strings.TrimSpace(a.Text) == "" || !careerDate(a.CreatedAt, true, false) ||
			!careerText(160, a.Provider) || !careerText(256, a.Model) || a.Provider == "" ||
			(a.Model == "" && a.Provider != "claude-cli" && a.Provider != "codex-cli") || a.Sources == nil ||
			len(a.Sources) > 1300 {
			return bad
		}
		identities[a.ID] = true
		citations := map[string]bool{}
		for _, v := range a.Sources {
			if !idPattern.MatchString(v.ID) || citations[v.ID] || !companies[v.CompanyID] || !careerText(512, v.Title) ||
				!careerLink(v.URL, false) || !careerDate(v.PublishedAt, false, true) || !careerDate(v.FetchedAt, true, false) {
				return bad
			}
			citations[v.ID] = true
		}
	}
	return nil
}

var careerTokens = regexp.MustCompile(`^(0|[1-9][0-9]*)$`)

func validateCareerUsage(u UsageSnapshot) error {
	bad := errors.New("Invalid model usage snapshot; retain explicit source dates and nonnegative token counts.")
	if u.Rows == nil || len(u.Rows) > 7*51 {
		return bad
	}
	empty := u.AsOf == "" && u.Version == "" && u.StartDate == "" && u.EndDate == "" && u.FetchedAt == "" && u.SourceURL == ""
	if empty {
		if len(u.Rows) == 0 {
			return nil
		}
		return bad
	}
	if !careerText(160, u.Version) || u.Version == "" || !careerDate(u.AsOf, true, false) ||
		!careerDate(u.StartDate, true, true) || !careerDate(u.EndDate, true, true) || len(u.StartDate) != 10 ||
		len(u.EndDate) != 10 || u.StartDate > u.EndDate || !careerDate(u.FetchedAt, true, false) ||
		!careerLink(u.SourceURL, true) {
		return bad
	}
	seen := map[string]bool{}
	for _, r := range u.Rows {
		key := r.Date + "\x00" + r.Model
		if !careerDate(r.Date, true, true) || len(r.Date) != 10 || r.Date < u.StartDate || r.Date > u.EndDate ||
			!careerText(512, r.Model) || r.Model == "" || len(r.TotalTokens) > 100 ||
			!careerTokens.MatchString(r.TotalTokens) || seen[key] {
			return bad
		}
		seen[key] = true
	}
	return nil
}

func normalizeCareer(c *CareerState) {
	if c.Companies == nil {
		c.Companies = []CareerCompany{}
	}
	if c.Jobs == nil {
		c.Jobs = []CareerJob{}
	}
	if c.Updates == nil {
		c.Updates = []CompanyUpdate{}
	}
	if c.Advice == nil {
		c.Advice = []CareerAdvice{}
	}
	if c.Usage.Rows == nil {
		c.Usage.Rows = []UsageRow{}
	}
	for i := range c.Companies {
		co := &c.Companies[i]
		co.Name = strings.TrimSpace(co.Name)
		if co.NewsURLs == nil {
			co.NewsURLs = []string{}
		}
		if co.ModelPrefixes == nil {
			co.ModelPrefixes = []string{}
		}
		if co.Refreshes == nil {
			co.Refreshes = []CareerRefresh{}
		}
		for _, p := range []*string{&co.Website, &co.CareersURL} {
			if *p != "" {
				if v, err := normalizeCareerURL(*p); err == nil {
					*p = v
				}
			}
		}
		for j := range co.NewsURLs {
			if v, err := normalizeCareerURL(co.NewsURLs[j]); err == nil {
				co.NewsURLs[j] = v
			}
		}
		sources := map[string]string{}
		if co.CareersURL != "" {
			sources[co.CareersURL] = "jobs"
		}
		for _, v := range co.NewsURLs {
			sources[v] = "updates"
		}
		co.Refreshes = slices.DeleteFunc(co.Refreshes, func(r CareerRefresh) bool {
			v, err := normalizeCareerURL(r.SourceURL)
			return err == nil && sources[v] != r.Kind
		})
		for j := range co.Refreshes {
			if v, err := normalizeCareerURL(co.Refreshes[j].SourceURL); err == nil {
				co.Refreshes[j].SourceURL = v
			}
		}
	}
	for i := range c.Jobs {
		j := &c.Jobs[i]
		for _, p := range []*string{&j.Level, &j.LevelBasis, &j.DateBasis, &j.ListingState} {
			if *p == "" {
				*p = "unknown"
			}
		}
	}
	for i := range c.Advice {
		if c.Advice[i].Sources == nil {
			c.Advice[i].Sources = []CareerCitation{}
		}
	}
}
func (s *Store) SaveCareer(c CareerState, expectedRevision int) (CareerState, error) {
	return s.saveCareer(context.Background(), c, expectedRevision, nil)
}

// saveCareer checks the operation at its commit boundary under the same lock as CAS.
// Cancellation after persistence commits does not roll back a saved write.
func (s *Store) saveCareer(ctx context.Context, c CareerState, expectedRevision int, settings *ModelSettings) (CareerState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ctx.Err() != nil {
		return CareerState{}, errors.New("Career operation canceled; nothing was saved.")
	}
	if settings != nil && s.state.Settings != *settings {
		return CareerState{}, ErrConflict
	}
	next := cloneState(s.state)
	if next.Career == nil || next.Career.Revision != expectedRevision {
		return CareerState{}, ErrConflict
	}
	// Copy before normalization so caller-owned slices are never mutated or stored.
	copy := c
	copy.Companies = slices.Clone(c.Companies)
	copy.Jobs = slices.Clone(c.Jobs)
	copy.Updates = slices.Clone(c.Updates)
	copy.Advice = slices.Clone(c.Advice)
	copy.Usage.Rows = slices.Clone(c.Usage.Rows)
	for i := range copy.Companies {
		copy.Companies[i].NewsURLs = slices.Clone(c.Companies[i].NewsURLs)
		copy.Companies[i].ModelPrefixes = slices.Clone(c.Companies[i].ModelPrefixes)
		copy.Companies[i].Refreshes = slices.Clone(c.Companies[i].Refreshes)
	}
	for i := range copy.Advice {
		copy.Advice[i].Sources = slices.Clone(c.Advice[i].Sources)
	}
	normalizeCareer(&copy)
	copy.Revision = expectedRevision + 1
	if err := validateCareer(copy); err != nil {
		return CareerState{}, err
	}
	next.Career = &copy
	if ctx.Err() != nil {
		return CareerState{}, errors.New("Career operation canceled; nothing was saved.")
	}
	if err := s.persist(next); err != nil {
		return CareerState{}, err
	}
	return *cloneState(next).Career, nil
}
