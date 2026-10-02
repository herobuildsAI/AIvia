package app

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestPublicIP(t *testing.T) {
	for _, ip := range []string{"127.0.0.1", "::1", "10.0.0.1", "169.254.169.254", "::ffff:127.0.0.1", "100.64.0.1", "198.18.0.1", "192.0.2.1", "2001:db8::1", "localhost", "8.8.8.8%en0"} {
		if publicIP(ip) {
			t.Errorf("accepted %s", ip)
		}
	}
	for _, ip := range []string{"8.8.8.8", "1.1.1.1", "2606:4700:4700::1111"} {
		if !publicIP(ip) {
			t.Errorf("rejected %s", ip)
		}
	}
}
func TestProviderIdentityAndUnknownFlags(t *testing.T) {
	b := []byte(`{"ip":"8.8.8.8","is_vpn":false,"is_abuser":null,"location":{"country_code":"US","timezone":"America/New_York"},"asn":{"asn":15169}}`)
	got, err := parseIntelligence(b, "8.8.8.8")
	if err != nil {
		t.Fatal(err)
	}
	if got.VPN == nil || *got.VPN || got.Abuse != nil || got.Proxy != nil {
		t.Fatal("lost false/missing distinction")
	}
	if _, err = parseIntelligence(b, "1.1.1.1"); err == nil {
		t.Fatal("accepted wrong IP")
	}
	if _, err = parseIntelligence([]byte(`{"ip":"8.8.8.8","is_vpn":"false"}`), "8.8.8.8"); err == nil {
		t.Fatal("accepted invalid flag type")
	}
}
func TestNetworkCancellationAndBounds(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/large" {
			w.Write([]byte(strings.Repeat("a", 2048)))
			return
		}
		<-r.Context().Done()
	}))
	defer ts.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, _, err := fetch(ctx, ts.Client(), http.MethodGet, ts.URL, nil, 1024); err == nil {
		t.Fatal("ignored cancel")
	}
	if _, _, err := fetch(context.Background(), ts.Client(), http.MethodGet, ts.URL+"/large", nil, 1024); err == nil {
		t.Fatal("accepted oversized response")
	}
}
func TestTargetRestrictions(t *testing.T) {
	for _, target := range []string{"https://127.0.0.1", "https://[::1]", "https://169.254.169.254"} {
		p := probeTarget(context.Background(), target, Options{})
		if p.State != "unknown" || p.Status != 0 {
			t.Fatalf("probed private target %#v", p)
		}
	}
	p := probeTarget(context.Background(), "https://example.com", Options{Proxy: "http://127.0.0.1:9"})
	if p.State != "unknown" || !strings.Contains(p.Summary, "proxy") {
		t.Fatalf("did not decline proxy route: %#v", p)
	}
}
func TestSystemNoCredentialValues(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://private-user:secret-password@localhost:1234")
	s := CollectSystem(context.Background())
	b, _ := json.Marshal(s)
	if strings.Contains(string(b), "private-user") || strings.Contains(string(b), "secret-password") {
		t.Fatal("proxy credentials leaked")
	}
	if s.OS == "" {
		t.Fatal("missing operating system")
	}
	if s.OffsetMinutes == nil && (s.Timezone != "Unknown" || !strings.Contains(strings.Join(s.Warnings, " "), "timezone")) {
		t.Fatal("missing timezone evidence was not explained as unknown")
	}
}

func TestProviderEquivalentAddresses(t *testing.T) {
	for _, pair := range [][2]string{{"2606:4700:4700::1111", "2606:4700:4700:0:0:0:0:1111"}, {"8.8.8.8", "::ffff:8.8.8.8"}} {
		body, _ := json.Marshal(map[string]any{"ip": pair[0], "is_vpn": false})
		if _, err := parseIntelligence(body, pair[1]); err != nil {
			t.Errorf("equivalent provider IP rejected: %v", err)
		}
	}
	if _, err := parseIntelligence([]byte(`{"ip":"invalid"}`), "invalid"); err == nil {
		t.Fatal("invalid matching strings accepted as an IP")
	}
}

func TestCustomIPIntelligenceRequestAndUnknownFlags(t *testing.T) {
	var calls atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var payload map[string]string
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		if r.Method != http.MethodPost || r.URL.Path != "/lookup" || r.URL.RawQuery != "" || r.Header.Get("Authorization") != "" || r.Header.Get("Content-Type") != "application/json" || payload["q"] != "8.8.8.8" || payload["key"] != "saved-secret" || len(payload) != 2 {
			t.Error("IP request did not use the compatible POST body protocol")
		}
		io.WriteString(w, `{"ip":"::ffff:8.8.8.8","is_vpn":false,"is_proxy":null,"location":{"country_code":"US"}}`)
	}))
	defer ts.Close()
	e := Evidence{Exits: []Exit{{IP: "8.8.8.8"}, {IP: "::ffff:8.8.8.8"}}}
	opts := Options{IPSettings: IPSettings{Provider: "custom", Endpoint: ts.URL + "/lookup", APIKey: "saved-secret"}, IPKey: "environment-secret"}
	collectIPIntelligence(context.Background(), &e, opts, nil)
	if calls.Load() != 1 || len(e.Warnings) != 0 || len(e.Exits) != 2 {
		t.Fatalf("lookup count %d warnings %v", calls.Load(), e.Warnings)
	}
	for _, x := range e.Exits {
		if x.Intel == nil || x.Intel.Country != "US" || x.Intel.VPN == nil || *x.Intel.VPN || x.Intel.Proxy != nil || x.Intel.Abuse != nil {
			t.Fatal("false, missing, or deduplicated lookup was lost")
		}
	}
	if e.IPSource == "" || strings.Contains(e.IPSource, "ipapi.is") || strings.Contains(e.IPSource, ts.URL) {
		t.Fatal("custom provenance misrepresented its provider or exposed the endpoint")
	}
}

func TestCustomIPIntelligenceFailureDoesNotLeakOrRedirect(t *testing.T) {
	var redirected atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirected.Add(1) }))
	defer destination.Close()
	for _, mode := range []string{"redirect", "http", "json", "mismatch", "provider-error"} {
		t.Run(mode, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch mode {
				case "redirect":
					http.Redirect(w, r, destination.URL+"/saved-secret", http.StatusTemporaryRedirect)
				case "http":
					http.Error(w, "saved-secret", http.StatusUnauthorized)
				case "json":
					io.WriteString(w, `saved-secret`)
				case "mismatch":
					io.WriteString(w, `{"ip":"1.1.1.1","is_proxy":false}`)
				case "provider-error":
					io.WriteString(w, `{"ip":"8.8.8.8","error":"saved-secret"}`)
				}
			}))
			defer ts.Close()
			e := Evidence{Exits: []Exit{{IP: "8.8.8.8"}}}
			collectIPIntelligence(context.Background(), &e, Options{IPSettings: IPSettings{Provider: "custom", Endpoint: ts.URL, APIKey: "saved-secret"}}, nil)
			if e.Exits[0].Intel != nil || len(e.Warnings) == 0 {
				t.Fatal("failed lookup did not remain unknown")
			}
			warnings := strings.Join(e.Warnings, " ")
			if strings.Contains(warnings, "saved-secret") || strings.Contains(warnings, ts.URL) {
				t.Fatal("lookup warning exposed provider data")
			}
		})
	}
	if redirected.Load() != 0 {
		t.Fatal("credential-bearing lookup followed a redirect")
	}
}

func TestCustomIPIntelligenceNeverFallsBackAroundProxy(t *testing.T) {
	var calls atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer ts.Close()
	for _, endpoint := range []string{ts.URL, "https://example.com/lookup"} {
		e := Evidence{Exits: []Exit{{IP: "8.8.8.8"}}}
		collectIPIntelligence(context.Background(), &e, Options{Proxy: "http://127.0.0.1:9", IPSettings: IPSettings{Provider: "custom", Endpoint: endpoint, APIKey: "saved-secret"}}, nil)
		if e.Exits[0].Intel != nil || len(e.Warnings) == 0 || !strings.Contains(strings.Join(e.Warnings, " "), "proxy") {
			t.Fatal("custom lookup did not explain unsupported proxy route")
		}
	}
	if calls.Load() != 0 {
		t.Fatal("custom lookup bypassed the configured proxy")
	}
}

func TestCustomIPClientPinsLoopbackDestination(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "ok") }))
	defer ts.Close()
	settings, err := NormalizeIPSettings(IPSettings{Provider: "custom", Endpoint: ts.URL})
	if err != nil {
		t.Fatal(err)
	}
	client, err := customIPClient(settings, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	u, _ := url.Parse(ts.URL)
	tr := client.Transport.(*http.Transport)
	for _, addr := range []string{net.JoinHostPort("localhost", u.Port()), "127.0.0.1:1", "8.8.8.8:443"} {
		if c, err := tr.DialContext(context.Background(), "tcp", addr); err == nil {
			c.Close()
			t.Fatal("custom transport accepted a changed destination")
		}
	}
}

func TestPublicAddressSetRejectsMixedDNS(t *testing.T) {
	if !publicAddresses([]net.IPAddr{{IP: net.ParseIP("8.8.8.8")}, {IP: net.ParseIP("2606:4700:4700::1111")}}) {
		t.Fatal("rejected public DNS answers")
	}
	for _, answers := range [][]net.IPAddr{nil, {{IP: net.ParseIP("8.8.8.8")}, {IP: net.ParseIP("127.0.0.1")}}, {{IP: net.ParseIP("::1")}}, {{IP: net.ParseIP("192.0.2.1")}}, {{IP: net.ParseIP("8.8.8.8"), Zone: "en0"}}} {
		if publicAddresses(answers) {
			t.Fatal("accepted private, reserved, scoped, empty, or mixed DNS answers")
		}
	}
}

func TestBuiltinIPIntelligenceUsesConfiguredProxy(t *testing.T) {
	var calls atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodConnect || r.Host != "api.ipapi.is:443" {
			t.Error("built-in request did not use its configured proxy and fixed destination")
		}
		http.Error(w, "proxy-private-message", http.StatusProxyAuthRequired)
	}))
	defer proxy.Close()
	opts := Options{Proxy: proxy.URL, IPKey: "environment-secret"}
	client, err := networkClient(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	e := Evidence{Exits: []Exit{{IP: "8.8.8.8"}}}
	collectIPIntelligence(context.Background(), &e, opts, client)
	if calls.Load() != 1 || e.Exits[0].Intel != nil || e.IPSource != "https://ipapi.is/developers.html" || len(e.Warnings) == 0 {
		t.Fatal("built-in proxy failure did not remain unknown")
	}
	if strings.Contains(strings.Join(e.Warnings, " "), "private-message") || strings.Contains(strings.Join(e.Warnings, " "), "environment-secret") {
		t.Fatal("provider failure leaked credential or response body")
	}
}

func TestIPIntelligenceMissingKeyStaysUnknown(t *testing.T) {
	e := Evidence{Exits: []Exit{{IP: "8.8.8.8"}}}
	collectIPIntelligence(context.Background(), &e, Options{}, nil)
	if e.Exits[0].Intel != nil || len(e.Warnings) == 0 {
		t.Fatal("missing built-in key did not remain unknown")
	}
}

func TestPublicDialRejectsMixedDNSBeforeConnecting(t *testing.T) {
	previous := net.DefaultResolver
	t.Cleanup(func() { net.DefaultResolver = previous })
	net.DefaultResolver = &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
		client, server := net.Pipe()
		go func() {
			defer server.Close()
			var size uint16
			if binary.Read(server, binary.BigEndian, &size) != nil {
				return
			}
			query := make([]byte, size)
			if _, err := io.ReadFull(server, query); err != nil {
				return
			}
			end := 12
			for end < len(query) && query[end] != 0 {
				end += int(query[end]) + 1
			}
			end += 5 // Null label, QTYPE, QCLASS.
			if end > len(query) {
				return
			}
			response := append([]byte(nil), query[:end]...)
			binary.BigEndian.PutUint16(response[2:4], 0x8180)
			binary.BigEndian.PutUint16(response[6:8], 0)
			binary.BigEndian.PutUint16(response[10:12], 0)
			if binary.BigEndian.Uint16(query[end-4:end-2]) == 1 {
				binary.BigEndian.PutUint16(response[6:8], 2)
				for _, ip := range [][4]byte{{8, 8, 8, 8}, {127, 0, 0, 1}} {
					response = append(response, 0xc0, 0x0c, 0, 1, 0, 1, 0, 0, 0, 0, 0, 4)
					response = append(response, ip[:]...)
				}
			}
			binary.Write(server, binary.BigEndian, uint16(len(response)))
			server.Write(response)
		}()
		return client, nil
	}}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	conn, err := publicDial(ctx, "tcp", "mixed.example.test:443")
	if conn != nil {
		conn.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "non-public") {
		t.Fatalf("mixed DNS was not rejected before dialing: %v", err)
	}
}
