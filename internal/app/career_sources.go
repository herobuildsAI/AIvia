package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const careerUsageEndpoint = "https://openrouter.ai/api/v1/datasets/rankings-daily"
const careerUsageSource = "https://openrouter.ai/rankings"

// fetchCareerUpdates consumes the route selected by careerSourceClient. Source
// authorship remains user-supplied; fetching a company URL does not verify it.
func fetchCareerUpdates(ctx context.Context, client *http.Client, source CareerSource) ([]CompanyUpdate, error) {
	resolved, err := resolveCareerSource(source.URL, "updates")
	if err != nil || resolved != source || client == nil {
		return nil, errors.New("Invalid updates source or unavailable source client.")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	safe := *client
	safe.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	defer safe.CloseIdleConnections()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, source.URL, nil)
	req.Header.Set("Accept", "application/rss+xml, application/atom+xml, application/xml, text/xml, text/html;q=0.9, text/plain;q=0.8")
	req.Header.Set("User-Agent", "AIvia/0.1 (public source reader)")
	body, media, err := readCareerSourceResponse(&safe, req, "Updates source")
	if err != nil {
		return nil, err
	}
	return parseCareerUpdates(source, media, body)
}

// readCareerSourceResponse never includes remote bodies, transport errors, or
// credentials in errors. The supplied client owns all routing/proxy decisions.
func readCareerSourceResponse(client *http.Client, req *http.Request, label string) ([]byte, string, error) {
	res, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("%s unavailable, canceled, or timed out.", label)
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, "", fmt.Errorf("%s returned HTTP %d; redirects are not followed.", label, res.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, (2<<20)+1))
	if err != nil || len(body) > 2<<20 || !utf8.Valid(body) {
		return nil, "", fmt.Errorf("%s response was unreadable, oversized, or not UTF-8.", label)
	}
	return body, res.Header.Get("Content-Type"), nil
}

func careerSourceMedia(raw string) (string, error) {
	media, params, err := mime.ParseMediaType(raw)
	charset := strings.ToLower(params["charset"])
	if err != nil || charset != "" && charset != "utf-8" && charset != "us-ascii" {
		return "", errors.New("Source must have a supported UTF-8 content type.")
	}
	return media, nil
}

// parseCareerUpdates does not crawl links. Plain HTML is deliberately retained
// as a page snapshot unless explicit publication metadata and article content
// occur together; a page title is not evidence of an article or its date.
func parseCareerUpdates(source CareerSource, contentType string, body []byte) ([]CompanyUpdate, error) {
	resolved, err := resolveCareerSource(source.URL, "updates")
	if err != nil || resolved != source {
		return nil, errors.New("Invalid updates source.")
	}
	if len(body) > 2<<20 || !utf8.Valid(body) {
		return nil, errors.New("Updates data exceeds 2 MiB or is not UTF-8.")
	}
	media, err := careerSourceMedia(contentType)
	if err != nil {
		return nil, err
	}
	switch media {
	case "application/rss+xml", "application/atom+xml", "application/xml", "text/xml":
		return parseCareerFeed(source, body)
	case "text/html", "text/plain":
		raw := string(body)
		title := "User-supplied news page"
		text := strings.TrimSpace(raw)
		kind, published := "page-snapshot", ""
		if media == "text/html" {
			if match := researchTitle.FindStringSubmatch(raw); len(match) > 1 {
				title = careerHTMLText(match[1])
			}
			text = researchNavigation.ReplaceAllString(researchVisibleHTML(raw), "\n")
			if match := researchMain.FindStringSubmatch(text); len(match) > 1 {
				text = match[1]
			}
			text = researchPlainText(text)
			if article, date := careerHTMLArticle(raw); date != "" {
				text, kind, published = article, "article", date
			}
		}
		if text == "" {
			return nil, errors.New("No readable update text found; paste the source text.")
		}
		if len(text) < 2048 {
			low := strings.ToLower(title + " " + text)
			for _, shell := range []string{"enable javascript", "javascript is required", "verify you are human", "checking your browser", "just a moment"} {
				if strings.Contains(low, shell) {
					return nil, errors.New("The updates page requires scripts or browser verification; paste the source text.")
				}
			}
		}
		if strings.TrimSpace(title) == "" {
			title = "User-supplied news page"
		}
		title, _ = boundedText(title, 512)
		text, truncated := boundedText(text, 6144)
		return []CompanyUpdate{{SourceID: careerUpdateIdentity(source.URL), SourceURL: source.URL, URL: source.URL, Title: title, Text: text, Kind: kind, PublishedAt: published, FetchedAt: timestamp(), Truncated: truncated}}, nil
	default:
		return nil, errors.New("Updates source returned an unsupported content type.")
	}
}

var careerArticleElement = regexp.MustCompile(`(?is)<article\b[^>]*>(.*?)</article\s*>`)
var careerArticleOpen = regexp.MustCompile(`(?i)<article\b`)
var careerMetaElement = regexp.MustCompile(`(?is)<meta\b[^>]*>`)
var careerHTMLAttribute = regexp.MustCompile(`(?is)\b([a-z_:][a-z0-9_:.-]*)\s*=\s*(?:"([^"]*)"|'([^']*)')`)

// Only a single visible article with one explicit publication meta qualifies.
// Missing, ambiguous, or malformed metadata leaves the page undated.
func careerHTMLArticle(raw string) (string, string) {
	visible := researchVisibleHTML(raw)
	if len(careerArticleOpen.FindAllStringIndex(visible, 2)) != 1 {
		return "", ""
	}
	articles := careerArticleElement.FindAllStringSubmatch(visible, 2)
	if len(articles) != 1 {
		return "", ""
	}
	text := researchPlainText(articles[0][1])
	if text == "" {
		return "", ""
	}
	// The visible-text helper removes head, where publication metadata lives.
	// Keep head for this separate metadata pass while stripping comments/scripts.
	metadata := researchComments.ReplaceAllString(raw, "\n")
	for _, pattern := range researchHidden[1:] {
		metadata = pattern.ReplaceAllString(metadata, "\n")
	}
	dates := []string{}
	for _, tag := range careerMetaElement.FindAllString(metadata, -1) {
		attrs := map[string]string{}
		for _, match := range careerHTMLAttribute.FindAllStringSubmatch(tag, -1) {
			key := strings.ToLower(match[1])
			value := match[2]
			if value == "" {
				value = match[3]
			}
			if _, duplicate := attrs[key]; duplicate {
				return "", ""
			}
			attrs[key] = html.UnescapeString(value)
		}
		if attrs["property"] == "article:published_time" {
			dates = append(dates, strings.TrimSpace(attrs["content"]))
		}
	}
	if len(dates) != 1 || dates[0] == "" {
		return "", ""
	}
	date, err := careerFeedDate(dates[0])
	if err != nil {
		return "", ""
	}
	return text, date
}

type careerXMLText struct {
	Raw string `xml:",innerxml"`
}
type careerFeedItem struct {
	ID    string        `xml:"id"`
	GUID  string        `xml:"guid"`
	Title careerXMLText `xml:"title"`
	Links []struct {
		Href string `xml:"href,attr"`
		Text string `xml:",chardata"`
		Rel  string `xml:"rel,attr"`
	} `xml:"link"`
	Description careerXMLText `xml:"description"`
	Content     careerXMLText `xml:"content"`
	Encoded     careerXMLText `xml:"encoded"`
	Summary     careerXMLText `xml:"summary"`
	Published   string        `xml:"published"`
	PubDate     string        `xml:"pubDate"`
}

func parseCareerFeed(source CareerSource, body []byte) ([]CompanyUpdate, error) {
	bad := errors.New("Invalid or oversized RSS/Atom feed.")
	decoder := xml.NewDecoder(bytes.NewReader(body))
	depth, roots, items := 0, 0, 0
	root := ""
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, bad
		}
		switch token := token.(type) {
		case xml.Directive:
			return nil, errors.New("XML directives and external entities are not supported.")
		case xml.ProcInst:
			if token.Target != "xml" || roots > 0 {
				return nil, bad
			}
		case xml.CharData:
			if depth == 0 && strings.TrimSpace(string(token)) != "" {
				return nil, bad
			}
		case xml.StartElement:
			if depth == 0 {
				roots++
				root = token.Name.Local
			}
			depth++
			if token.Name.Local == "item" || token.Name.Local == "entry" {
				items++
			}
			if depth > 32 || roots > 1 || items > 5000 {
				return nil, bad
			}
		case xml.EndElement:
			depth--
		}
	}
	if roots != 1 || depth != 0 || root != "rss" && root != "feed" {
		return nil, bad
	}
	// The token pass rejects directives and bounds depth/items before decoding.
	var records []careerFeedItem
	if root == "rss" {
		var feed struct {
			Items []careerFeedItem `xml:"channel>item"`
		}
		if err := xml.Unmarshal(body, &feed); err != nil {
			return nil, bad
		}
		records = feed.Items
	} else {
		var feed struct {
			Entries []careerFeedItem `xml:"entry"`
		}
		if err := xml.Unmarshal(body, &feed); err != nil {
			return nil, bad
		}
		records = feed.Entries
	}
	out := []CompanyUpdate{}
	ids, urls := map[string]bool{}, map[string]bool{}
	fetched := timestamp()
	for _, record := range records {
		id, title, link, date := strings.TrimSpace(record.ID), careerFeedText(record.Title.Raw), "", strings.TrimSpace(record.Published)
		if root == "rss" {
			id = strings.TrimSpace(record.GUID)
			date = strings.TrimSpace(record.PubDate)
			if len(record.Links) > 0 {
				link = strings.TrimSpace(record.Links[0].Text)
			}
		} else {
			link = ""
			for _, candidate := range record.Links {
				if candidate.Rel == "" || candidate.Rel == "alternate" {
					link = candidate.Href
					break
				}
			}
		}
		if title == "" || !careerText(512, id, title) || strings.ContainsFunc(id, unicode.IsControl) {
			return nil, bad
		}
		published, err := careerFeedDate(date)
		if err != nil {
			return nil, err
		}
		safeLink := ""
		if link != "" {
			base, _ := url.Parse(source.URL)
			ref, err := url.Parse(link)
			if err == nil {
				safeLink, _ = normalizeCareerURL(base.ResolveReference(ref).String())
			}
		}
		if id == "" {
			if safeLink != "" {
				id = careerUpdateIdentity(safeLink)
			} else {
				id = careerUpdateIdentity(source.URL + "\x00" + title)
			}
		}
		if ids[id] || safeLink != "" && urls[safeLink] {
			continue
		}
		ids[id] = true
		if safeLink != "" {
			urls[safeLink] = true
		}

		text := ""
		for _, candidate := range []string{record.Content.Raw, record.Encoded.Raw, record.Description.Raw, record.Summary.Raw} {
			if candidate != "" {
				text = careerFeedText(candidate)
				break
			}
		}
		text, truncated := boundedText(text, 6144)
		if len(out) < 20 {
			out = append(out, CompanyUpdate{SourceID: id, SourceURL: source.URL, URL: safeLink, Title: title, Text: text, Kind: "feed-item", PublishedAt: published, FetchedAt: fetched, Truncated: truncated})
		}
	}
	return out, nil
}

func careerFeedText(raw string) string {
	// CDATA contains literal text/HTML; remove its XML wrapper before the shared
	// text sanitizer, which would otherwise treat the whole wrapper as a tag.
	raw = strings.ReplaceAll(strings.ReplaceAll(raw, "<![CDATA[", ""), "]]>", "")
	return careerHTMLText(raw)
}

func careerUpdateIdentity(raw string) string {
	if len(raw) <= 512 && !strings.ContainsFunc(raw, unicode.IsControl) {
		return raw
	}
	return fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(raw)))
}

func careerFeedDate(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	for _, layout := range []string{time.RFC3339, time.RFC1123Z, time.RFC1123, time.RFC822Z, time.RFC822, "2006-01-02"} {
		if parsed, err := time.Parse(layout, raw); err == nil {
			if layout == "2006-01-02" {
				return raw, nil
			}
			return parsed.UTC().Format(time.RFC3339), nil
		}
	}
	return "", errors.New("Invalid feed publication date; previous updates are retained.")
}

// fetchCareerUsage requests seven completed UTC dates. The key exists only on
// this fixed dataset request; redirects are refused even for the same host.
func fetchCareerUsage(ctx context.Context, client *http.Client, key string, now time.Time) (UsageSnapshot, error) {
	if client == nil || strings.TrimSpace(key) == "" || !careerText(512, key) || strings.ContainsFunc(key, unicode.IsControl) {
		return UsageSnapshot{}, errors.New("Configure a valid OpenRouter key and source client.")
	}
	today := now.UTC().Truncate(24 * time.Hour)
	start, end := today.AddDate(0, 0, -7).Format("2006-01-02"), today.AddDate(0, 0, -1).Format("2006-01-02")
	query := url.Values{"start_date": {start}, "end_date": {end}}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	safe := *client
	safe.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	defer safe.CloseIdleConnections()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, careerUsageEndpoint+"?"+query.Encode(), nil)
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Accept", "application/json")
	body, contentType, err := readCareerSourceResponse(&safe, req, "OpenRouter dataset")
	if err != nil {
		return UsageSnapshot{}, err
	}
	if media, err := careerSourceMedia(contentType); err != nil || media != "application/json" {
		return UsageSnapshot{}, errors.New("OpenRouter dataset returned an unsupported content type.")
	}
	snapshot, err := parseCareerUsage(body)
	if err != nil {
		return UsageSnapshot{}, err
	}
	if snapshot.StartDate != start || snapshot.EndDate != end {
		return UsageSnapshot{}, errors.New("OpenRouter dataset returned a different date window; previous snapshot is retained.")
	}
	return snapshot, nil
}

func parseCareerUsage(body []byte) (UsageSnapshot, error) {
	bad := errors.New("Invalid OpenRouter dataset; previous snapshot is retained.")
	if len(body) > 2<<20 || !utf8.Valid(body) {
		return UsageSnapshot{}, bad
	}
	var payload struct {
		Data []struct {
			Date   string `json:"date"`
			Model  string `json:"model_permaslug"`
			Tokens string `json:"total_tokens"`
		} `json:"data"`
		Meta struct {
			AsOf    string `json:"as_of"`
			Version string `json:"version"`
			Start   string `json:"start_date"`
			End     string `json:"end_date"`
		} `json:"meta"`
	}
	if json.Unmarshal(body, &payload) != nil || payload.Data == nil {
		return UsageSnapshot{}, bad
	}
	snapshot := UsageSnapshot{Rows: []UsageRow{}, AsOf: payload.Meta.AsOf, Version: payload.Meta.Version, StartDate: payload.Meta.Start, EndDate: payload.Meta.End, FetchedAt: timestamp(), SourceURL: careerUsageSource}
	start, e1 := time.Parse("2006-01-02", snapshot.StartDate)
	end, e2 := time.Parse("2006-01-02", snapshot.EndDate)
	if e1 != nil || e2 != nil || end.Before(start) || end.Sub(start) > 6*24*time.Hour || len(payload.Data) > 7*51 {
		return UsageSnapshot{}, bad
	}
	daily := map[string]int{}
	for _, row := range payload.Data {
		daily[row.Date]++
		if daily[row.Date] > 51 || strings.TrimSpace(row.Model) == "" || strings.ContainsFunc(row.Model, unicode.IsControl) {
			return UsageSnapshot{}, bad
		}
		snapshot.Rows = append(snapshot.Rows, UsageRow{Date: row.Date, Model: row.Model, TotalTokens: row.Tokens})
	}
	if validateCareerUsage(snapshot) != nil {
		return UsageSnapshot{}, bad
	}
	return snapshot, nil
}
