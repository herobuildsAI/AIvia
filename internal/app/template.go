package app

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

// ServiceTemplate deliberately excludes local IDs, notes, issues and reports.
type ServiceTemplate struct {
	Format  string `json:"format"`
	Version int    `json:"version"`
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Origin  string `json:"origin"`
	Policy  Policy `json:"policy"`
}

func templateDraft(v ServiceTemplate) (Profile, error) {
	if v.Format != "aivia-service-template" || v.Version != 1 {
		return Profile{}, errors.New("Use an AIvia service template with format aivia-service-template and version 1. Profile/report exports are not templates.")
	}
	p := v.Policy
	if p.Mode != "unknown" && p.Mode != "custom" && p.Mode != "worldwide" || p.Builtin != "" || p.Provenance != "" {
		return Profile{}, errors.New("Templates support unknown, custom, or worldwide policies only. Built-in identities and verification claims cannot be imported.")
	}
	if p.Mode == "unknown" && (p.Source != "" || p.CheckedAt != "" || len(p.Countries) != 0 || len(p.ExcludedRegions) != 0) || p.Mode == "worldwide" && len(p.Countries) != 0 {
		return Profile{}, errors.New("Unknown policies must omit rule data. Worldwide policies must omit the supported-country list.")
	}
	draft := Profile{Name: v.Name, Kind: v.Kind, Origin: v.Origin, Policy: p}
	if err := normalizeProfile(&draft); err != nil {
		return Profile{}, err
	}
	for _, regions := range draft.Policy.ExcludedRegions {
		for _, region := range regions {
			if strings.ContainsAny(region, ",\r\n") {
				return Profile{}, errors.New("Excluded region names cannot contain commas or line breaks in the service editor.")
			}
		}
	}
	// Validate the URL syntax without resolving or contacting any host.
	for _, raw := range []string{draft.Origin, draft.Policy.Source} {
		if raw != "" {
			if _, err := validateResearchURL(raw); err != nil {
				return Profile{}, err
			}
		}
	}
	return draft, nil
}

func (s *Server) previewTemplate(w http.ResponseWriter, r *http.Request) {
	var v ServiceTemplate
	if !readJSON(w, r, &v) {
		return
	}
	draft, err := templateDraft(v)
	if err != nil {
		fail(w, err)
		return
	}
	// Preview never writes to the store or refreshes the policy review date.
	writeJSON(w, http.StatusOK, draft)
}

func (s *Server) exportTemplate(w http.ResponseWriter, r *http.Request) {
	p, err := s.profile(r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	policy := resolvePolicy(p.Policy)
	if policy.Mode == "builtin" {
		policy.Mode = "custom"
	}
	policy.Builtin, policy.Provenance = "", ""
	if policy.Mode == "unknown" {
		policy = Policy{Mode: "unknown"}
	}
	v := ServiceTemplate{Format: "aivia-service-template", Version: 1, Name: p.Name, Kind: p.Kind, Origin: p.Origin, Policy: policy}
	if _, err = templateDraft(v); err != nil {
		fail(w, err)
		return
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil || len(data) > 256<<10 {
		fail(w, errors.New("This template exceeds 256 KiB. Reduce its policy data before sharing."))
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="service-template.json"`)
	writeJSON(w, http.StatusOK, v)
}
