package app

import (
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

// ProxyFromEnvironment caches its configuration. Each case needs a new process
// so it exercises the same startup boundary as the application.
func TestProxyConfigurationNeverSilentlyBecomesDirect(t *testing.T) {
	if os.Getenv("AIVIA_PROXY_TEST_CHILD") == "1" {
		req, err := http.NewRequest(http.MethodGet, "https://example.test/policy", nil)
		if err != nil {
			t.Fatal(err)
		}
		client, err := networkClient(Options{})
		var proxy *url.URL
		if err == nil {
			defer client.CloseIdleConnections()
			proxy, err = client.Transport.(*http.Transport).Proxy(req)
		}
		want := os.Getenv("AIVIA_PROXY_TEST_WANT")
		if want == "error" {
			if err == nil {
				t.Fatal("malformed proxy permitted a request")
			}
			if strings.Contains(err.Error(), "bad%zz") {
				t.Fatal("proxy validation disclosed credentials")
			}
		} else if err != nil || (proxy != nil) != (want == "proxy") {
			t.Fatalf("unexpected routing decision: proxy=%t error=%v", proxy != nil, err)
		}
		for name, factory := range map[string]func() (*http.Client, error){
			"research": func() (*http.Client, error) {
				return researchClient(Options{}, []string{req.URL.String()})
			},
			"custom provider": func() (*http.Client, error) {
				return customIPClient(IPSettings{Provider: "custom", Endpoint: "https://example.test/lookup"}, Options{})
			},
		} {
			c, e := factory()
			if c != nil {
				c.CloseIdleConnections()
			}
			if (e != nil) != (want != "direct") {
				t.Errorf("%s: incorrect refusal of configured proxy: %v", name, e)
			}
		}
		// Local compatible providers retain the standard loopback bypass.
		local, err := customIPClient(IPSettings{Provider: "custom", Endpoint: "http://127.0.0.1:1234/lookup"}, Options{})
		if err != nil {
			t.Fatalf("loopback provider was blocked: %v", err)
		}
		local.CloseIdleConnections()
		return
	}
	caseVariantWant := "proxy"
	if runtime.GOOS == "windows" {
		// Environment keys are case insensitive; os/exec keeps the last value.
		caseVariantWant = "error"
	}
	cases := []struct {
		name, want string
		env        []string
	}{
		{"invalid-credentials", "error", []string{"HTTPS_PROXY=http://user:bad%zz@127.0.0.1:7890"}},
		{"invalid-scheme", "error", []string{"HTTPS_PROXY=ftp://127.0.0.1:7890"}},
		{"invalid-port", "error", []string{"HTTPS_PROXY=http://127.0.0.1:99999"}},
		{"invalid-query", "error", []string{"HTTPS_PROXY=http://127.0.0.1:7890?token=fixture"}},
		{"bare-host", "proxy", []string{"HTTPS_PROXY=127.0.0.1:7890"}},
		{"lowercase", "proxy", []string{"https_proxy=http://127.0.0.1:7890"}},
		{"case-variant-precedence", caseVariantWant, []string{"HTTPS_PROXY=http://127.0.0.1:7890", "https_proxy=invalid://"}},
		{"no-proxy", "direct", []string{"HTTPS_PROXY=http://127.0.0.1:7890", "NO_PROXY=example.test"}},
		{"no-proxy-all", "direct", []string{"HTTPS_PROXY=http://127.0.0.1:7890", "NO_PROXY=*"}},
		{"unrelated-http-setting", "direct", []string{"HTTP_PROXY=http://user:bad%zz@127.0.0.1:7890"}},
		{"unset", "direct", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=^TestProxyConfigurationNeverSilentlyBecomesDirect$")
			for _, entry := range os.Environ() {
				key, _, _ := strings.Cut(entry, "=")
				switch strings.ToUpper(key) {
				case "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY", "REQUEST_METHOD", "AIVIA_PROXY_TEST_CHILD", "AIVIA_PROXY_TEST_WANT":
					continue
				}
				cmd.Env = append(cmd.Env, entry)
			}
			cmd.Env = append(cmd.Env, tc.env...)
			cmd.Env = append(cmd.Env, "AIVIA_PROXY_TEST_CHILD=1", "AIVIA_PROXY_TEST_WANT="+tc.want)
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("proxy subprocess failed: %s", output)
			}
		})
	}
}
