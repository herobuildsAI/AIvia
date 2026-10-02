package app

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
)

func TestCollectNetworkWithLocalProviders(t *testing.T) {
	if os.Getenv("AIVIA_COLLECT_TEST_CHILD") != "1" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestCollectNetworkWithLocalProviders$")
		for _, entry := range os.Environ() {
			key, _, _ := strings.Cut(entry, "=")
			switch strings.ToUpper(key) {
			case "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY", "REQUEST_METHOD", "AIVIA_COLLECT_TEST_CHILD":
				continue
			}
			cmd.Env = append(cmd.Env, entry)
		}
		cmd.Env = append(cmd.Env, "AIVIA_COLLECT_TEST_CHILD=1")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("network fixture subprocess failed: %s", output)
		}
		return
	}
	for _, mode := range []string{"success", "ipv6-unavailable", "provider-unavailable"} {
		t.Run(mode, func(t *testing.T) {
			var mu sync.Mutex
			lookups := map[string]int{}
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.Host {
				case "api.ipify.org":
					io.WriteString(w, `{"ip":"8.8.8.8"}`)
				case "api6.ipify.org":
					if mode == "ipv6-unavailable" {
						w.WriteHeader(http.StatusServiceUnavailable)
						return
					}
					io.WriteString(w, `{"ip":"2606:4700:4700::1111"}`)
				case "www.cloudflare.com":
					if r.Method != http.MethodHead {
						t.Error("clock observation did not use HEAD")
					}
				case "api.ipapi.is":
					var input map[string]string
					if json.NewDecoder(r.Body).Decode(&input) != nil || input["key"] != "fixture-key" || r.Method != http.MethodPost {
						t.Error("unexpected intelligence request")
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					mu.Lock()
					lookups[input["q"]]++
					mu.Unlock()
					if mode == "provider-unavailable" {
						http.Error(w, "fixture-key", http.StatusServiceUnavailable)
						return
					}
					json.NewEncoder(w).Encode(map[string]any{"ip": input["q"], "is_vpn": false, "location": map[string]string{"country_code": "US"}})
				default:
					t.Error("unexpected provider destination")
					w.WriteHeader(http.StatusBadRequest)
				}
			}))
			defer server.Close()
			// Replace only the socket/TLS boundary; real collector URLs, payloads,
			// parsing, deduplication and failure handling still run end to end.
			transport := server.Client().Transport.(*http.Transport).Clone()
			transport.TLSClientConfig.InsecureSkipVerify = true // Local fixture certificate represents several provider names.
			transport.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
				var dialer net.Dialer
				return dialer.DialContext(ctx, "tcp", server.Listener.Addr().String())
			}
			previous := http.DefaultTransport
			http.DefaultTransport = transport
			defer func() { http.DefaultTransport = previous; transport.CloseIdleConnections() }()
			in := RunInput{Network: true, Intelligence: true, Exits: []Exit{{Path: "browser", Family: "ipv4", IP: "8.8.8.8", Intel: &Intelligence{Country: "CN"}}}}
			e := CollectNetwork(context.Background(), in, Options{IPKey: "fixture-key"})
			if len(e.Exits) != 3 || !e.Network || e.ClockSkewSeconds == nil || math.Abs(*e.ClockSkewSeconds) > 5 || e.Target.State != "unknown" {
				t.Fatalf("collector lost observations: exits=%d clock=%v", len(e.Exits), e.ClockSkewSeconds)
			}
			for _, exit := range e.Exits {
				if mode == "provider-unavailable" || mode == "ipv6-unavailable" && exit.Family == "ipv6" {
					if exit.Intel != nil {
						t.Fatal("failed observation acquired reputation evidence")
					}
					if mode == "ipv6-unavailable" && (exit.IP != "" || exit.Error == "") {
						t.Fatal("failed egress was not retained as unknown")
					}
					continue
				}
				if exit.Intel == nil || exit.Intel.Country != "US" || exit.Intel.VPN == nil || *exit.Intel.VPN || exit.Intel.Abuse != nil {
					t.Fatal("collector changed observed, false, or unknown intelligence")
				}
			}
			mu.Lock()
			defer mu.Unlock()
			if lookups["8.8.8.8"] != 1 {
				t.Fatal("shared browser/agent address was not deduplicated")
			}
			if mode == "provider-unavailable" && (len(lookups) != 1 || len(e.Warnings) == 0 || strings.Contains(strings.Join(e.Warnings, " "), "fixture-key")) {
				t.Fatal("provider failure retried or leaked its response")
			}
		})
	}
}
