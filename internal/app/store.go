package app

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

var ErrConflict = errors.New("This record changed. Reload it before saving.")
var idPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)

const maxStoreSize = 16 << 20

type Store struct {
	mu     sync.Mutex
	dir    string
	state  State
	closed bool
}

func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
func cloneState(s State) State {
	b, _ := json.Marshal(s)
	var c State
	_ = json.Unmarshal(b, &c)
	return c
}
func OpenStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(filepath.Join(dir, "store.lock"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, errors.New("Data directory is locked. Close other instances. If a previous process crashed, verify it has stopped before removing store.lock.")
	}
	_, err = fmt.Fprintf(lock, "pid=%d\nstarted=%s\n", os.Getpid(), timestamp())
	closeErr := lock.Close()
	if err != nil || closeErr != nil {
		_ = os.Remove(filepath.Join(dir, "store.lock"))
		return nil, errors.New("Could not write the data directory lock.")
	}
	s := &Store{dir: dir, state: State{Version: 1, Profiles: []Profile{}, Reports: []RedactedReport{}, Settings: ModelSettings{Provider: "ollama", Endpoint: "http://127.0.0.1:11434"}}}
	success := false
	defer func() {
		if !success {
			_ = s.Close()
		}
	}()
	path := filepath.Join(dir, "store.json")
	if st, e := os.Lstat(path); e == nil && !st.Mode().IsRegular() {
		return nil, errors.New("The data store must be a regular file.")
	}
	f, err := os.Open(path)
	if err == nil {
		defer f.Close()
		b, e := io.ReadAll(io.LimitReader(f, maxStoreSize+1))
		if e != nil || len(b) > maxStoreSize {
			return nil, errors.New("Data store cannot be read or exceeds 16 MiB; original preserved.")
		}
		s.state = State{} // Existing files must supply their own schema and collections.
		dec := json.NewDecoder(strings.NewReader(string(b)))
		dec.DisallowUnknownFields()
		if e = dec.Decode(&s.state); e != nil {
			return nil, errors.New("Data store is corrupt or incompatible; original preserved. See recovery instructions.")
		}
		if e = dec.Decode(new(any)); e != io.EOF {
			return nil, errors.New("Data store contains trailing data; original preserved.")
		}
		if e = validateState(s.state); e != nil {
			return nil, fmt.Errorf("Data store rejected; original preserved: %w", e)
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	} else if _, e := os.Stat(filepath.Join(dir, "store.json.bak")); e == nil {
		return nil, errors.New("Store missing but backup exists; restore store.json.bak before starting.")
	}
	success = true
	return s, nil
}
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	return os.Remove(filepath.Join(s.dir, "store.lock"))
}
func (s *Store) Snapshot() State { s.mu.Lock(); defer s.mu.Unlock(); return cloneState(s.state) }
func validateOrigin(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || (u.Path != "" && u.Path != "/") || (u.Port() != "" && u.Port() != "443") || strings.ContainsAny(u.Host, "\\\r\n\t ") {
		return "", errors.New("Use a public HTTPS origin, without credentials, paths, query parameters, or a non-443 port.")
	}
	u.Path = ""
	u.RawPath = ""
	u.Host = strings.ToLower(u.Host)
	return u.String(), nil
}
func normalizeProfile(p *Profile) error {
	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" || utf8.RuneCountInString(p.Name) > 160 || len(p.Notes) > 8192 {
		return errors.New("A name of 1–160 characters is required; notes are limited to 8 KiB.")
	}
	if !slices.Contains([]string{"website", "app"}, p.Kind) {
		return errors.New("Kind must be website or app.")
	}
	if p.Status == "" {
		p.Status = "unchecked"
	}
	if !slices.Contains([]string{"unchecked", "investigating", "blocked", "working"}, p.Status) {
		return errors.New("Invalid service status.")
	}
	var err error
	p.Origin, err = validateOrigin(p.Origin)
	if err != nil {
		return err
	}
	if p.Policy.Mode == "" {
		p.Policy.Mode = "unknown"
	}
	if err = validatePolicy(p.Policy); err != nil {
		return err
	}
	if len(p.Issues) > 50 {
		return errors.New("A service can contain at most 50 issues.")
	}
	seen := map[string]bool{}
	for i := range p.Issues {
		v := &p.Issues[i]
		if v.ID == "" {
			v.ID = newID()
		}
		if !idPattern.MatchString(v.ID) || seen[v.ID] {
			return errors.New("Invalid or duplicate issue ID.")
		}
		seen[v.ID] = true
		if !slices.Contains([]string{"registration", "login", "access"}, v.Stage) || len(v.Error) > 8192 || len(v.Notes) > 8192 {
			return errors.New("Invalid issue stage or issue text exceeds 8 KiB.")
		}
		if v.ObservedAt == "" {
			v.ObservedAt = timestamp()
		}
		if _, err := time.Parse(time.RFC3339, v.ObservedAt); err != nil {
			return errors.New("Issue date must be an RFC3339 timestamp.")
		}
	}
	if p.Issues == nil {
		p.Issues = []Issue{}
	}
	return nil
}
func validatePolicy(p Policy) error {
	if !slices.Contains([]string{"unknown", "builtin", "custom", "worldwide"}, p.Mode) {
		return errors.New("Invalid region policy mode.")
	}
	if p.Mode == "builtin" && !slices.Contains([]string{"claude-web", "claude-api"}, p.Builtin) {
		return errors.New("Unknown starter policy.")
	}
	if p.Mode == "custom" || p.Mode == "worldwide" {
		u, e := url.Parse(p.Source)
		if e != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return errors.New("Policy source must be an HTTPS URL without credentials, query parameters, or fragments.")
		}
		if _, e = time.Parse("2006-01-02", p.CheckedAt); e != nil {
			return errors.New("A policy review date (YYYY-MM-DD) is required.")
		}
		if p.Mode == "custom" && len(p.Countries) == 0 {
			return errors.New("Enter supported country codes, or explicitly select worldwide.")
		}
	}
	if len(p.Countries) > 250 || len(p.ExcludedRegions) > 250 {
		return errors.New("Policy contains too many regions.")
	}
	for _, c := range p.Countries {
		if !validCountry(c) {
			return errors.New("Country codes must be uppercase ISO two-letter codes.")
		}
	}
	for c, regions := range p.ExcludedRegions {
		if !validCountry(c) || len(regions) > 30 {
			return errors.New("Invalid regional exclusions.")
		}
		for _, r := range regions {
			if strings.TrimSpace(r) == "" || len(r) > 100 {
				return errors.New("Invalid excluded region.")
			}
		}
	}
	return nil
}
func validateState(st State) error {
	if st.Version != 1 || st.Profiles == nil || st.Reports == nil {
		return errors.New("Unsupported schema version.")
	}
	if _, err := NormalizeIPSettings(st.IPSettings); err != nil {
		return err
	}
	if len(st.Profiles) > 200 || len(st.Reports) > 200 {
		return errors.New("Store capacity exceeded.")
	}
	ids := map[string]bool{}
	for _, p := range st.Profiles {
		if !idPattern.MatchString(p.ID) || ids[p.ID] || p.Revision < 1 {
			return errors.New("Invalid profile identity.")
		}
		ids[p.ID] = true
		if err := normalizeProfile(&p); err != nil {
			return err
		}
	}
	reportIDs := map[string]bool{}
	for _, r := range st.Reports {
		if !ids[r.ProfileID] || !idPattern.MatchString(r.ID) || reportIDs[r.ID] || r.Lower < 0 || r.Upper > 100 || r.Lower > r.Upper || len(r.Findings) > 32 {
			return errors.New("Invalid saved report.")
		}
		reportIDs[r.ID] = true
	}
	return nil
}
func (s *Store) persist(next State) error {
	if s.closed {
		return errors.New("Store is closed.")
	}
	if err := validateState(next); err != nil {
		return err
	}
	b, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return err
	}
	if len(b) > maxStoreSize {
		return errors.New("Local data limit (16 MiB) reached. Export and delete records before adding more.")
	}
	path := filepath.Join(s.dir, "store.json")
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return errors.New("Could not create temporary store. Original preserved; check permissions or a leftover store.json.tmp.")
	}
	defer os.Remove(tmp)
	if _, err = f.Write(b); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if old, e := os.ReadFile(path); e == nil {
		backup := path + ".bak"
		if info, e := os.Lstat(backup); e == nil && !info.Mode().IsRegular() {
			return errors.New("Backup path is not a regular file.")
		}
		if err = os.WriteFile(backup, old, 0600); err != nil {
			return err
		}
	} else if !os.IsNotExist(e) {
		return e
	}
	if err = os.Rename(tmp, path); err != nil {
		return errors.New("Could not replace store. Previous data and backup preserved.")
	}
	s.state = next
	return nil
}
func (s *Store) SaveProfile(p Profile) (Profile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := cloneState(s.state)
	index := -1
	for i, v := range next.Profiles {
		if v.ID == p.ID {
			index = i
			break
		}
	}
	if p.ID != "" && index < 0 {
		return Profile{}, errors.New("Service not found.")
	}
	if index >= 0 {
		old := next.Profiles[index]
		if old.Revision != p.Revision {
			return Profile{}, ErrConflict
		}
		p.CreatedAt = old.CreatedAt
		p.Revision++
	} else {
		if len(next.Profiles) >= 200 {
			return Profile{}, errors.New("Service limit (200) reached.")
		}
		p.ID = newID()
		p.Revision = 1
		p.CreatedAt = timestamp()
	}
	if err := normalizeProfile(&p); err != nil {
		return Profile{}, err
	}
	p.UpdatedAt = timestamp()
	if index < 0 {
		next.Profiles = append(next.Profiles, p)
	} else {
		next.Profiles[index] = p
	}
	if err := s.persist(next); err != nil {
		return Profile{}, err
	}
	return p, nil
}
func (s *Store) DeleteProfile(id string, revision int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := cloneState(s.state)
	index := slices.IndexFunc(next.Profiles, func(p Profile) bool { return p.ID == id })
	if index < 0 {
		return errors.New("Service not found.")
	}
	if next.Profiles[index].Revision != revision {
		return ErrConflict
	}
	next.Profiles = append(next.Profiles[:index], next.Profiles[index+1:]...)
	next.Reports = slices.DeleteFunc(next.Reports, func(r RedactedReport) bool { return r.ProfileID == id })
	return s.persist(next)
}
func (s *Store) SaveReport(r RedactedReport) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := cloneState(s.state)
	for _, old := range next.Reports {
		if old.ID == r.ID {
			return nil
		}
	}
	if len(next.Reports) >= 200 {
		return errors.New("Saved report limit (200) reached. Export or delete an older report first.")
	}
	next.Reports = append(next.Reports, r)
	return s.persist(next)
}
func (s *Store) DeleteReport(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := cloneState(s.state)
	next.Reports = slices.DeleteFunc(next.Reports, func(r RedactedReport) bool { return r.ID == id })
	return s.persist(next)
}
func (s *Store) SaveSettings(v ModelSettings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := cloneState(s.state)
	next.Settings = v
	return s.persist(next)
}
