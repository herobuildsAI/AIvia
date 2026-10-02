package app

import (
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
)

func (s *Server) ipSettingsView(v IPSettings) map[string]any {
	effective, err := effectiveIPSettings(v, s.opts.IPKey)
	if err != nil {
		return map[string]any{"ready": false, "error": "IP settings are invalid."}
	}
	source := "none"
	if v.APIKey != "" {
		source = "saved on this device"
	} else if effective.APIKey != "" {
		source = "AIVPN_IPAPI_KEY environment variable"
	}
	return map[string]any{"provider": effective.Provider, "endpoint": effective.Endpoint, "revision": effective.Revision, "keyConfigured": effective.APIKey != "", "keySource": source, "ready": effective.Provider == "custom" || effective.APIKey != ""}
}

func (s *Server) saveIPSettings(w http.ResponseWriter, r *http.Request) {
	var input struct {
		IPSettings
		ClearKey bool `json:"clearKey"`
	}
	if !readJSON(w, r, &input) {
		return
	}
	next, err := NormalizeIPSettings(input.IPSettings)
	if err != nil {
		fail(w, err)
		return
	}
	old, err := NormalizeIPSettings(s.store.Snapshot().IPSettings)
	if err != nil {
		fail(w, err)
		return
	}
	if next.Revision != old.Revision {
		fail(w, ErrConflict)
		return
	}
	if input.ClearKey && next.APIKey != "" {
		fail(w, errors.New("Clear the key field before removing a saved key."))
		return
	}
	if !input.ClearKey && next.APIKey == "" && next.Provider == old.Provider && next.Endpoint == old.Endpoint {
		next.APIKey = old.APIKey
	}
	u, _ := url.Parse(next.Endpoint)
	_, ownPort, _ := net.SplitHostPort(s.opts.Host)
	port := u.Port()
	if port == "" {
		if u.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	if ip := net.ParseIP(u.Hostname()); ip != nil && ip.IsLoopback() && port == ownPort {
		fail(w, errors.New("The IP endpoint cannot use this application's port."))
		return
	}
	if err = s.store.SaveIPSettings(next); err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, s.ipSettingsView(s.store.Snapshot().IPSettings))
}

func normalizeModelSettings(v *ModelSettings) error {
	v.Model = strings.TrimSpace(v.Model)
	v.CLIPath = strings.TrimSpace(v.CLIPath)
	if len(v.Model) > 256 || len(v.CLIPath) > 4096 {
		return errors.New("Model ID or executable path is too long.")
	}
	if v.Provider == "claude-cli" || v.Provider == "codex-cli" {
		v.Endpoint = ""
		v.LocalConfirmed = false
		if v.Model == "" {
			v.Model = "default"
		}
	} else {
		v.CLIPath = ""
		v.CloudConfirmed = false
	}
	return nil
}
