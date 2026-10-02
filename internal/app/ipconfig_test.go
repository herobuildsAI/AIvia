package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeIPSettings(t *testing.T) {
	for _, tt := range []struct {
		in                 IPSettings
		provider, endpoint string
	}{
		{IPSettings{}, "ipapi", "https://api.ipapi.is/"},
		{IPSettings{Provider: "ipapi", Endpoint: "https://API.IPAPI.IS:443"}, "ipapi", "https://api.ipapi.is/"},
		{IPSettings{Provider: "custom", Endpoint: " HTTPS://Example.COM.:443/check "}, "custom", "https://example.com/check"},
		{IPSettings{Provider: "custom", Endpoint: "http://[::ffff:127.0.0.1]:80"}, "custom", "http://127.0.0.1/"},
		{IPSettings{Provider: "custom", Endpoint: "https://[::1]:8443/v1/lookup"}, "custom", "https://[::1]:8443/v1/lookup"},
	} {
		got, err := NormalizeIPSettings(tt.in)
		if err != nil || got.Provider != tt.provider || got.Endpoint != tt.endpoint {
			t.Errorf("NormalizeIPSettings(%#v) = %#v, %v", tt.in, got, err)
		}
	}
}

func TestIPSettingsRejectUnsafeEndpoints(t *testing.T) {
	for _, endpoint := range []string{
		"", "http://example.com", "https://example.com:8443/x", "https://example.com:0",
		"https://u:secret@example.com", "https://example.com/?key=secret", "https://example.com/?", "https://example.com/#secret", "https://example.com/#",
		"https://10.0.0.1", "https://169.254.169.254", "https://100.64.0.1", "https://192.0.2.1", "https://[2001:db8::1]",
		"http://localhost:8080", "https://localhost", "https://[fe80::1%25en0]", "http://127.0.0.1:65536", "https://example.com:",
	} {
		if _, err := NormalizeIPSettings(IPSettings{Provider: "custom", Endpoint: endpoint}); err == nil {
			t.Errorf("accepted endpoint %q", endpoint)
		} else if strings.Contains(err.Error(), "secret") {
			t.Fatal("credential was reflected in validation error")
		}
	}
	for _, v := range []IPSettings{
		{Provider: "unknown"}, {Provider: "ipapi", Endpoint: "https://example.com/"},
		{APIKey: "secret\nkey"}, {APIKey: strings.Repeat("x", 4097)}, {Revision: -1},
	} {
		if _, err := NormalizeIPSettings(v); err == nil {
			t.Fatal("accepted invalid settings")
		}
	}
}

func TestEffectiveIPSettingsDoesNotSendEnvironmentKeyToCustom(t *testing.T) {
	got, err := effectiveIPSettings(IPSettings{}, "environment-secret")
	if err != nil || got.APIKey != "environment-secret" || got.Provider != "ipapi" {
		t.Fatal("legacy environment key is unavailable")
	}
	got, err = effectiveIPSettings(IPSettings{APIKey: "saved-secret"}, "environment-secret")
	if err != nil || got.APIKey != "saved-secret" {
		t.Fatal("saved key did not take precedence")
	}
	got, err = effectiveIPSettings(IPSettings{Provider: "custom", Endpoint: "http://127.0.0.1:8080/lookup"}, "environment-secret")
	if err != nil || got.APIKey != "" {
		t.Fatal("built-in environment key escaped to custom provider")
	}
}

func TestIPSettingsStoreRoundTripAndRevision(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	v := IPSettings{Provider: "custom", Endpoint: "http://127.0.0.1:8080/lookup", APIKey: "saved-secret"}
	if err := s.SaveIPSettings(v); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveIPSettings(v); !errors.Is(err, ErrConflict) {
		t.Fatal("stale IP settings overwrote saved settings")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got := s.Snapshot().IPSettings
	if got.Revision != 1 || got.APIKey != "saved-secret" || got.Endpoint != v.Endpoint {
		t.Fatal("IP settings did not survive reopening")
	}
	if info, err := os.Stat(filepath.Join(dir, "store.json")); err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("credentials lack private store permissions")
	}
	got.Endpoint = "https://10.0.0.1"
	if err := s.SaveIPSettings(got); err == nil {
		t.Fatal("saved unsafe endpoint")
	}
	if s.Snapshot().IPSettings.Endpoint != v.Endpoint {
		t.Fatal("failed save changed stored endpoint")
	}
}

func TestIPSettingsLegacyStoreDefaults(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "store.json"), []byte(`{"version":1,"profiles":[],"reports":[],"settings":{}}`), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err := effectiveIPSettings(s.Snapshot().IPSettings, "legacy-key")
	if err != nil || got.Endpoint != "https://api.ipapi.is/" || got.APIKey != "legacy-key" || got.Revision != 0 {
		t.Fatal("legacy store lost the built-in provider")
	}
}
