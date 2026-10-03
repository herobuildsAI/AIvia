package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// CareerSource authorizes only a recognized fixed ATS or a strict arbitrary page.
type CareerSource struct{ URL, Kind, Provider, Board, JobID string }
type CareerJobBatch struct {
	Source           CareerSource
	Jobs             []CareerJob
	Complete         bool
	Scope, CheckedAt string
}

var careerBoardToken = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,159}$`)
var careerPostingToken = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,159}$`)
var careerGreenhouseID = regexp.MustCompile(`^[1-9][0-9]*$`)

func resolveCareerSource(raw, kind string) (CareerSource, error) {
	bad := errors.New("Use a supported public careers URL; query-based company wrappers require pasted text.")
	if kind != "jobs" && kind != "updates" {
		return CareerSource{}, bad
	}
	normalized, err := normalizeCareerURL(raw)
	if err != nil {
		return CareerSource{}, err
	}
	u, _ := url.Parse(normalized)
	source := CareerSource{URL: normalized, Kind: kind, Provider: "custom"}
	if kind == "jobs" {
		switch u.Hostname() {
		case "boards.greenhouse.io", "job-boards.greenhouse.io":
			source.Provider = "greenhouse"
		case "jobs.ashbyhq.com":
			source.Provider = "ashby"
		case "jobs.lever.co":
			source.Provider = "lever"
		case "jobs.eu.lever.co":
			source.Provider = "lever-eu"
		}
	}
	if source.Provider == "custom" {
		if _, err = validateResearchURL(normalized); err != nil {
			return CareerSource{}, bad
		}
		return source, nil
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) == 0 || !careerBoardToken.MatchString(parts[0]) || strings.Contains(u.EscapedPath(), "%") {
		return CareerSource{}, bad
	}
	source.Board = parts[0]
	if source.Provider == "greenhouse" {
		if len(parts) == 3 && parts[1] == "jobs" {
			source.JobID = parts[2]
		} else if len(parts) != 1 {
			return CareerSource{}, bad
		}
	} else {
		if len(parts) == 3 && (source.Provider == "ashby" && parts[2] == "application" || (source.Provider == "lever" || source.Provider == "lever-eu") && parts[2] == "apply") {
			parts = parts[:2]
		}
		if len(parts) == 2 {
			source.JobID = parts[1]
		} else if len(parts) != 1 {
			return CareerSource{}, bad
		}
	}
	if source.JobID != "" && (!careerPostingToken.MatchString(source.JobID) || source.Provider == "greenhouse" && !careerGreenhouseID.MatchString(source.JobID)) {
		return CareerSource{}, bad
	}
	for key, values := range u.Query() {
		// Hosted source attribution does not select/filter postings. All other keys reject.
		allowed := key == "utm_source" || key == "utm_medium" || key == "utm_campaign" || key == "utm_term" || key == "utm_content" || source.Provider == "greenhouse" && key == "gh_src"
		if !allowed || len(values) != 1 || len(values[0]) > 512 {
			return CareerSource{}, bad
		}
	}
	u.RawQuery = ""
	u.ForceQuery = false
	u.Path = "/" + strings.Join(parts, "/")
	source.URL = u.String()
	return source, nil
}

// careerSourceClient is the route boundary: callers pass their actual server options.
// Custom sources retain public-DNS enforcement and proxy refusal; ATS reads honor proxies.
func careerSourceClient(opts Options, source CareerSource) (*http.Client, error) {
	resolved, err := resolveCareerSource(source.URL, source.Kind)
	if err != nil || resolved != source {
		return nil, errors.New("Invalid public source.")
	}
	if source.Provider == "custom" {
		return researchClient(opts, []string{source.URL})
	}
	client, err := networkClient(opts)
	if err == nil {
		// Large fixed ATS boards need the fetch deadline through the body read.
		// Keep networkClient's header, TLS, and proxy transport boundaries.
		client.Timeout = 30 * time.Second
	}
	return client, err
}

// fetchCareerJobs consumes a client from careerSourceClient; it never substitutes routes.
func fetchCareerJobs(ctx context.Context, client *http.Client, source CareerSource) (CareerJobBatch, error) {
	resolved, err := resolveCareerSource(source.URL, source.Kind)
	if err != nil || resolved != source || client == nil {
		return CareerJobBatch{}, errors.New("Invalid recruiting source or unavailable source client.")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	safe := *client
	safe.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if source.Provider == "custom" {
		body, err := readCareerResponse(ctx, &safe, source.URL, false, 2<<20)
		if err != nil {
			return CareerJobBatch{}, err
		}
		return parseCareerJobs(source, body)
	}
	endpoint := ""
	switch source.Provider {
	case "greenhouse":
		endpoint = "https://boards-api.greenhouse.io/v1/boards/" + source.Board + "/jobs"
		if source.JobID != "" {
			endpoint += "/" + source.JobID
		} else {
			endpoint += "?content=true"
		}
	case "ashby":
		endpoint = "https://api.ashbyhq.com/posting-api/job-board/" + source.Board
	case "lever", "lever-eu":
		host := "api.lever.co"
		if source.Provider == "lever-eu" {
			host = "api.eu.lever.co"
		}
		endpoint = "https://" + host + "/v0/postings/" + source.Board
		if source.JobID != "" {
			endpoint += "/" + source.JobID
		}
	}
	if source.Provider != "lever" && source.Provider != "lever-eu" || source.JobID != "" {
		body, e := readCareerResponse(ctx, &safe, endpoint, true, 16<<20)
		if e != nil {
			return CareerJobBatch{}, e
		}
		return parseCareerJobs(source, body)
	}
	out := CareerJobBatch{Source: source, Jobs: []CareerJob{}, Scope: "board", CheckedAt: timestamp()}
	seen := map[string]bool{}
	for page := 0; page < 5; page++ {
		q := url.Values{"mode": {"json"}, "skip": {strconv.Itoa(page * 100)}, "limit": {"100"}}
		body, e := readCareerResponse(ctx, &safe, endpoint+"?"+q.Encode(), true, 16<<20)
		if e != nil {
			return CareerJobBatch{}, e
		}
		b, e := parseCareerJobs(source, body)
		if e != nil {
			return CareerJobBatch{}, e
		}
		if len(b.Jobs) > 100 {
			return CareerJobBatch{}, errors.New("Recruiting page exceeded its requested size.")
		}
		for _, j := range b.Jobs {
			if seen[j.ProviderID] {
				return CareerJobBatch{}, errors.New("Recruiting pages repeated a posting ID.")
			}
			seen[j.ProviderID] = true
			j.FetchedAt = out.CheckedAt
			out.Jobs = append(out.Jobs, j)
		}
		if !b.Complete {
			return out, nil
		}
		if len(b.Jobs) < 100 {
			out.Complete = true
			return out, nil
		}
	}
	return out, nil // Reaching the 500-result boundary never proves a complete board.
}

func readCareerResponse(ctx context.Context, client *http.Client, endpoint string, jsonBody bool, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, errors.New("Invalid recruiting destination.")
	}
	req.Header.Set("User-Agent", "AIvia/0.1 (public source reader)")
	req.Header.Set("Accept-Language", "en")
	accept := "text/html, text/plain;q=0.9"
	if jsonBody {
		accept = "application/json"
	}
	req.Header.Set("Accept", accept)
	res, err := client.Do(req)
	if err != nil {
		return nil, errors.New("Recruiting source unavailable, canceled, or timed out.")
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("Recruiting source returned HTTP %d; redirects are not followed.", res.StatusCode)
	}
	media, params, err := mime.ParseMediaType(res.Header.Get("Content-Type"))
	if err != nil || jsonBody && media != "application/json" || !jsonBody && media != "text/html" && media != "text/plain" {
		return nil, errors.New("Recruiting source returned an unsupported content type.")
	}
	if charset := strings.ToLower(params["charset"]); charset != "" && charset != "utf-8" && charset != "us-ascii" {
		return nil, errors.New("Recruiting source must use UTF-8.")
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
			return nil, errors.New("Recruiting response read was canceled; retry the refresh or paste the source text.")
		}
		var timeout net.Error
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.As(err, &timeout) && timeout.Timeout() {
			return nil, errors.New("Recruiting response read timed out; retry the refresh or paste the source text.")
		}
		return nil, errors.New("Recruiting response could not be read; retry the refresh or paste the source text.")
	}
	if int64(len(body)) > limit {
		return nil, errors.New("Recruiting response exceeded its size limit; use a single posting or paste the source text.")
	}
	if !utf8.Valid(body) {
		return nil, errors.New("Recruiting response is not valid UTF-8; paste the source text.")
	}
	return body, nil
}

type careerATSJob struct {
	ID               json.RawMessage                       `json:"id"`
	Title            string                                `json:"title"`
	Text             string                                `json:"text"`
	Location         json.RawMessage                       `json:"location"`
	Categories       struct{ Location, Commitment string } `json:"categories"`
	Content          string                                `json:"content"`
	DescriptionPlain string                                `json:"descriptionPlain"`
	DescriptionHTML  string                                `json:"descriptionHtml"`
	Description      string                                `json:"description"`
	AdditionalPlain  string                                `json:"additionalPlain"`
	Additional       string                                `json:"additional"`
	Lists            []struct{ Text, Content string }      `json:"lists"`
	URL              string                                `json:"absolute_url"`
	JobURL           string                                `json:"jobUrl"`
	HostedURL        string                                `json:"hostedUrl"`
	Published        string                                `json:"publishedAt"`
	FirstPublished   string                                `json:"first_published"`
	Updated          string                                `json:"updated_at"`
	Workplace        string                                `json:"workplaceType"`
	Employment       string                                `json:"employmentType"`
	Listed           *bool                                 `json:"isListed"`
}

func parseCareerJobs(source CareerSource, body []byte) (CareerJobBatch, error) {
	out := CareerJobBatch{Source: source, Jobs: []CareerJob{}, Complete: true, Scope: "board", CheckedAt: timestamp()}
	if source.JobID != "" {
		out.Scope = "posting"
	}
	if !utf8.Valid(body) {
		return out, errors.New("Recruiting data must use UTF-8.")
	}
	if source.Provider == "paste" || source.Provider == "custom" {
		if len(body) > 2<<20 {
			return out, errors.New("Pasted or custom recruiting text exceeds 2 MiB.")
		}
		return parseCareerPage(out, string(body))
	}
	if len(body) > 16<<20 {
		return out, errors.New("Recruiting response exceeds 16 MiB.")
	}
	var records []careerATSJob
	if (source.Provider == "greenhouse" || source.Provider == "lever" || source.Provider == "lever-eu") && source.JobID != "" {
		var job careerATSJob
		if err := json.Unmarshal(body, &job); err != nil {
			return out, errors.New("Invalid recruiting posting.")
		}
		records = []careerATSJob{job}
	} else if source.Provider == "lever" || source.Provider == "lever-eu" {
		if err := json.Unmarshal(body, &records); err != nil || records == nil {
			return out, errors.New("Missing recruiting posting collection.")
		}
	} else if source.Provider == "greenhouse" || source.Provider == "ashby" {
		var board struct {
			Jobs []careerATSJob `json:"jobs"`
			Meta struct {
				Total *int `json:"total"`
			} `json:"meta"`
		}
		if err := json.Unmarshal(body, &board); err != nil || board.Jobs == nil {
			return out, errors.New("Missing recruiting posting collection.")
		}
		records = board.Jobs
		if board.Meta.Total != nil && *board.Meta.Total != len(records) {
			out.Complete = false
		}
	} else {
		return out, errors.New("Unsupported recruiting provider.")
	}
	if len(records) >= 500 && source.JobID == "" {
		out.Complete = false
	}
	seen := map[string]careerATSJob{}
	for _, record := range records {
		id := careerNativeID(record.ID)
		if source.Provider == "ashby" && record.Listed == nil {
			return out, errors.New("Ashby posting is missing its listing flag.")
		}
		if record.Listed != nil && !*record.Listed {
			continue
		}
		if id == "" || len(id) > 512 || source.Provider == "greenhouse" && !careerGreenhouseID.MatchString(id) {
			return out, errors.New("Recruiting posting has no valid native identity.")
		}
		if old, ok := seen[id]; ok {
			if !reflect.DeepEqual(old, record) {
				return out, errors.New("Conflicting recruiting posting identities.")
			}
			out.Complete = false
			continue
		}
		seen[id] = record
		if source.JobID != "" && source.JobID != id {
			if source.Provider != "ashby" {
				return out, errors.New("Recruiting posting identity did not match.")
			}
			continue
		}
		j := CareerJob{Provider: source.Provider, ProviderID: id, SourceURL: source.URL, Title: record.Title, Workplace: record.Workplace, EmploymentType: record.Employment, FetchedAt: out.CheckedAt, ListingState: "listed"}
		switch source.Provider {
		case "greenhouse":
			var location struct{ Name string }
			if len(record.Location) > 0 && string(record.Location) != "null" && json.Unmarshal(record.Location, &location) != nil {
				return out, errors.New("Invalid recruiting location.")
			}
			j.Location = location.Name
			j.Text = careerHTMLText(record.Content)
			j.URL = record.URL
			j.PublishedAt = record.FirstPublished
			j.UpdatedAt = record.Updated
		case "ashby":
			if len(record.Location) > 0 && string(record.Location) != "null" && json.Unmarshal(record.Location, &j.Location) != nil {
				return out, errors.New("Invalid recruiting location.")
			}
			j.Text = record.DescriptionPlain
			if j.Text == "" {
				j.Text = careerHTMLText(record.DescriptionHTML)
			}
			j.URL = record.JobURL
			j.PublishedAt = record.Published
		case "lever", "lever-eu":
			j.Title = record.Text
			j.Location = record.Categories.Location
			j.EmploymentType = record.Categories.Commitment
			j.Text = record.DescriptionPlain
			if j.Text == "" {
				j.Text = careerHTMLText(record.Description)
			}
			for _, list := range record.Lists {
				j.Text += "\n" + list.Text + "\n" + careerHTMLText(list.Content)
			}
			additional := record.AdditionalPlain
			if additional == "" {
				additional = careerHTMLText(record.Additional)
			}
			j.Text += "\n" + additional
			j.URL = record.HostedURL
		}
		if err := finishCareerJob(&j); err != nil {
			return out, err
		}
		if len(out.Jobs) < 500 {
			out.Jobs = append(out.Jobs, j)
		}
	}
	if source.JobID != "" && len(out.Jobs) == 0 {
		return out, errors.New("The requested listed posting was not present.")
	}
	return out, nil
}
func careerNativeID(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.TrimSpace(s)
	}
	var n json.Number
	if json.Unmarshal(raw, &n) == nil {
		v, e := strconv.ParseUint(n.String(), 10, 64)
		if e == nil && v > 0 {
			return n.String()
		}
	}
	return ""
}
func careerHTMLText(raw string) string {
	return researchPlainText(html.UnescapeString(html.UnescapeString(raw)))
}

var careerInternSignal = regexp.MustCompile(`(?i)\b(intern|internship|internships)\b`)
var careerGraduateSignal = regexp.MustCompile(`(?i)\b(new[ -]grad(uate)?s?|recent[ -]graduates?|graduate[ -](engineer|developer|program|role)|university[ -]graduate)\b`)
var careerEntrySignal = regexp.MustCompile(`(?i)\bentry[ -]level\b`)

var careerBodyLevelSignals = []struct {
	re    *regexp.Regexp
	level string
}{
	{regexp.MustCompile(`(?i)\b(this (is an?|role is an?)|position[: ]+) internship\b`), "internship"},
	{regexp.MustCompile(`(?i)\b(new|recent) graduates (are )?(welcome|encouraged|eligible)\b`), "new-graduate"},
	{regexp.MustCompile(`(?i)\b(entry[ -]level (position|role|opportunity)|position is entry[ -]level)\b`), "entry-level"},
}

func careerLevel(title, employment, text string) (string, string) {
	for _, signal := range []struct {
		re    *regexp.Regexp
		level string
	}{{careerInternSignal, "internship"}, {careerGraduateSignal, "new-graduate"}, {careerEntrySignal, "entry-level"}} {
		if signal.re.MatchString(title) {
			return signal.level, "title-inferred"
		}
	}
	if careerInternSignal.MatchString(employment) {
		return "internship", "posting-explicit"
	}
	// Only explicit role statements classify body text; incidental benefits or references do not.
	for _, signal := range careerBodyLevelSignals {
		if signal.re.MatchString(text) {
			return signal.level, "posting-explicit"
		}
	}

	return "unknown", "unknown"
}
func finishCareerJob(j *CareerJob) error {
	j.Title = strings.TrimSpace(j.Title)
	if j.Title == "" || !careerText(512, j.Title, j.Location, j.Workplace, j.EmploymentType) {
		return errors.New("Recruiting posting contains missing or oversized fields.")
	}
	j.Text = strings.TrimSpace(j.Text)
	j.Level, j.LevelBasis = careerLevel(j.Title, j.EmploymentType, j.Text)
	j.Text, j.Truncated = boundedText(j.Text, 6144)
	j.DateBasis = "unknown"
	if j.PublishedAt != "" {
		if !careerDate(j.PublishedAt, false, true) {
			return errors.New("Invalid recruiting publication date.")
		}
		j.DateBasis = "published"
		if j.Provider == "ashby" {
			j.DateBasis = "last-published"
		}
	}
	if j.UpdatedAt != "" {
		if !careerDate(j.UpdatedAt, false, true) {
			return errors.New("Invalid recruiting update date.")
		}
		if j.DateBasis == "unknown" {
			j.DateBasis = "updated"
		}
	}
	if j.URL != "" {
		if normalized, err := normalizeCareerURL(j.URL); err == nil {
			j.URL = normalized
		} else {
			j.URL = ""
		}
	}
	return nil
}

var careerJSONLDScript = regexp.MustCompile(`(?is)<script\b([^>]*)>(.*?)</script\s*>`)
var careerJSONLDType = regexp.MustCompile(`(?i)\btype\s*=\s*["']application/ld\+json["']`)

func parseCareerPage(out CareerJobBatch, raw string) (CareerJobBatch, error) {
	out.Complete = false
	out.Scope = "posting"
	if out.Source.Provider == "paste" {
		out.Scope = "paste"
	}
	if out.Source.Provider == "custom" {
		visited := 0
		var visit func(any, int) error
		visit = func(value any, depth int) error {
			visited++
			if visited > 1000 || depth > 8 {
				return errors.New("Structured job data exceeded traversal limits.")
			}
			switch v := value.(type) {
			case []any:
				for _, child := range v {
					if err := visit(child, depth+1); err != nil {
						return err
					}
				}
			case map[string]any:
				if types, ok := v["@type"].([]any); ok {
					for _, item := range types {
						if _, ok := item.(string); !ok {
							return errors.New("Structured job type must be a flat string array.")
						}
					}
				}
				if locations, ok := v["jobLocation"].([]any); ok {
					for _, item := range locations {
						if _, ok := item.(map[string]any); !ok {
							return errors.New("Structured job locations must be a flat place array.")
						}
					}
				}
				if careerJSONLDJobType(v["@type"]) {
					j := CareerJob{Provider: "custom", SourceURL: out.Source.URL, Title: careerJSONLDString(v["title"]), Text: careerHTMLText(careerJSONLDString(v["description"])), URL: careerJSONLDString(v["url"]), PublishedAt: careerJSONLDString(v["datePosted"]), EmploymentType: careerJSONLDString(v["employmentType"]), Workplace: careerJSONLDString(v["jobLocationType"]), FetchedAt: out.CheckedAt, ListingState: "unknown"}
					j.Location = careerJSONLDLocation(v["jobLocation"])
					if j.URL != "" {
						base, _ := url.Parse(out.Source.URL)
						ref, e := url.Parse(j.URL)
						if e == nil {
							j.URL = base.ResolveReference(ref).String()
						}
					}
					if err := finishCareerJob(&j); err != nil {
						return err
					}
					j.ProviderID = j.URL
					if j.ProviderID == "" {
						j.ProviderID = careerJSONLDIdentifier(v["identifier"])
					}
					if j.ProviderID == "" {
						j.ProviderID = out.Source.URL
					}
					if len(j.ProviderID) > 512 {
						j.ProviderID = ""
					}
					if len(out.Jobs) < 500 {
						out.Jobs = append(out.Jobs, j)
					}
				}
				for key, child := range v {
					if key == "@context" {
						continue
					}
					if err := visit(child, depth+1); err != nil {
						return err
					}
				}
			}
			return nil
		}
		for _, script := range careerJSONLDScript.FindAllStringSubmatch(raw, -1) {
			if !careerJSONLDType.MatchString(script[1]) {
				continue
			}
			var value any
			if json.Unmarshal([]byte(script[2]), &value) != nil {
				return out, errors.New("Invalid structured recruiting data.")
			}
			if err := visit(value, 0); err != nil {
				return out, err
			}
		}
		if len(out.Jobs) > 0 {
			return out, nil
		}
	}
	title := "Pasted job posting"
	text := strings.TrimSpace(raw)
	if out.Source.Provider == "custom" {
		title = "Careers page extract"
		if match := researchTitle.FindStringSubmatch(raw); len(match) > 1 {
			title = careerHTMLText(match[1])
		}
		text = researchNavigation.ReplaceAllString(researchVisibleHTML(raw), "\n")
		if match := researchMain.FindStringSubmatch(text); len(match) > 1 {
			text = match[1]
		}
		text = researchPlainText(text)
	} else if line, _, ok := strings.Cut(text, "\n"); ok {
		title = line
	} else {
		title = text
	}
	if text == "" {
		return out, errors.New("No readable job text found; paste the posting text.")
	}
	if out.Source.Provider == "custom" && len(text) < 2048 {
		low := strings.ToLower(title + " " + text)
		for _, shell := range []string{"enable javascript", "javascript is required", "verify you are human", "checking your browser", "just a moment"} {
			if strings.Contains(low, shell) {
				return out, errors.New("The careers page requires scripts or browser verification; paste the posting text.")
			}
		}
	}
	title, _ = boundedText(title, 512)
	j := CareerJob{Provider: out.Source.Provider, SourceURL: out.Source.URL, URL: out.Source.URL, Title: title, Text: text, FetchedAt: out.CheckedAt, ListingState: "unknown"}
	if err := finishCareerJob(&j); err != nil {
		return out, err
	}
	out.Jobs = append(out.Jobs, j)
	return out, nil
}
func careerJSONLDJobType(v any) bool {
	jobType := func(v any) bool {
		s, ok := v.(string)
		return ok && (s == "JobPosting" || s == "https://schema.org/JobPosting")
	}
	if jobType(v) {
		return true
	}
	if values, ok := v.([]any); ok {
		for _, value := range values {
			if jobType(value) {
				return true
			}
		}
	}
	return false
}
func careerJSONLDString(v any) string { s, _ := v.(string); return s }
func careerJSONLDIdentifier(v any) string {
	if property, ok := v.(map[string]any); ok {
		v = property["value"]
	}
	id := strings.TrimSpace(careerJSONLDString(v))
	if !careerText(512, id) || strings.ContainsFunc(id, unicode.IsControl) {
		return ""
	}
	// URL identifiers use the same public, credential-free boundary as links.
	u, err := url.Parse(id)
	if err != nil {
		return ""
	}
	if u.Scheme != "" || u.Host != "" {
		id, err = normalizeCareerURL(id)
		if err != nil || len(id) > 512 {
			return ""
		}
	}
	return id
}
func careerJSONLDLocation(v any) string {
	place := func(value any) string {
		m, _ := value.(map[string]any)
		a, _ := m["address"].(map[string]any)
		parts := []string{}
		for _, k := range []string{"addressLocality", "addressRegion", "addressCountry"} {
			if s := careerJSONLDString(a[k]); s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, ", ")
	}
	if values, ok := v.([]any); ok {
		parts := []string{}
		for _, value := range values {
			if s := place(value); s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, "; ")
	}
	return place(v)
}

func careerJobIdentity(j CareerJob) string {
	source, err := resolveCareerSource(j.SourceURL, "jobs")
	board := j.SourceURL
	if err == nil && source.Provider != "custom" {
		board = source.Board
	}
	id := j.ProviderID
	if j.Provider == "custom" {
		if normalized, e := normalizeCareerURL(j.SourceURL); e == nil {
			board = normalized
		}
		if id == "" {
			id = j.URL
		}
	}
	if j.Provider == "paste" {
		id = j.ID
	}
	return j.CompanyID + "\x00" + j.Provider + "\x00" + board + "\x00" + id
}
func mergeCareerJobs(current CareerState, companyID string, batch CareerJobBatch) (CareerState, error) {
	if !slices.ContainsFunc(current.Companies, func(c CareerCompany) bool { return c.ID == companyID }) || !careerDate(batch.CheckedAt, true, false) || len(batch.Jobs) > 500 || !careerEnum(batch.Scope, "board", "posting", "paste") {
		return CareerState{}, errors.New("Invalid recruiting merge or company.")
	}
	if batch.Source.Provider != "paste" {
		resolved, err := resolveCareerSource(batch.Source.URL, "jobs")
		if err != nil || resolved != batch.Source {
			return CareerState{}, errors.New("Invalid recruiting batch source.")
		}
	} else if batch.Scope != "paste" || batch.Source.URL != "" {
		return CareerState{}, errors.New("Invalid pasted recruiting source.")
	}
	next := current
	next.Jobs = slices.Clone(current.Jobs)
	indexes := map[string]int{}
	for i, j := range next.Jobs {
		indexes[careerJobIdentity(j)] = i
	}
	seen := map[string]bool{}
	for _, j := range batch.Jobs {
		if j.Provider != batch.Source.Provider || j.SourceURL != batch.Source.URL {
			return CareerState{}, errors.New("Recruiting posting source did not match batch.")
		}
		j.CompanyID = companyID
		j.FetchedAt = batch.CheckedAt
		if j.ID == "" {
			j.ID = newID()
		}
		key := careerJobIdentity(j)
		if seen[key] {
			return CareerState{}, errors.New("Duplicate posting in recruiting merge.")
		}
		seen[key] = true
		if i, ok := indexes[key]; ok {
			j.ID = next.Jobs[i].ID
			next.Jobs[i] = j
		} else {
			indexes[key] = len(next.Jobs)
			next.Jobs = append(next.Jobs, j)
		}
	}
	if batch.Complete && batch.Scope == "board" && batch.Source.Provider != "custom" && batch.Source.Provider != "paste" {
		resolved, err := resolveCareerSource(batch.Source.URL, "jobs")
		if err != nil || resolved != batch.Source || batch.Source.JobID != "" {
			return CareerState{}, errors.New("Invalid board snapshot.")
		}
		for i, j := range next.Jobs {
			old, err := resolveCareerSource(j.SourceURL, "jobs")
			if j.CompanyID == companyID && j.Provider == batch.Source.Provider && j.ListingState == "listed" && err == nil && old.Board == batch.Source.Board && !seen[careerJobIdentity(j)] {
				next.Jobs[i].ListingState = "no-longer-listed"
			}
		}
	}
	// Validate capacity and all incoming fields before returning; caller-owned slices remain untouched.
	if err := validateCareer(next); err != nil {
		return CareerState{}, err
	}
	return next, nil
}
