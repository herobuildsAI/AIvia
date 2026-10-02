package app

import (
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
)

func parseProxyURL(raw string, allowBareHost bool) (*url.URL, error) {
	if allowBareHost && !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "socks5" && u.Scheme != "socks5h") || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return nil, errors.New("Invalid proxy. Use an HTTP, HTTPS, or SOCKS5 proxy URL.")
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return nil, errors.New("Invalid proxy port.")
		}
	}
	return u, nil
}

// Validate before asking net/http for routing: it silently ignores some malformed
// environment values. Valid configurations retain its NO_PROXY matching rules.
// Loopback stays local even when a remote proxy setting is malformed.
func validatedProxyFromEnvironment(req *http.Request) (*url.URL, error) {
	host := req.URL.Hostname()
	if strings.EqualFold(host, "localhost") || net.ParseIP(host).IsLoopback() {
		return nil, nil
	}
	key := "HTTP_PROXY"
	if req.URL.Scheme == "https" {
		key = "HTTPS_PROXY"
	}
	raw := os.Getenv(key)
	if raw == "" {
		raw = os.Getenv(strings.ToLower(key))
	}
	if raw != "" {
		if _, err := parseProxyURL(raw, true); err != nil {
			return nil, errors.New("Invalid " + key + " configuration. Fix or unset it before making network requests.")
		}
	}
	return http.ProxyFromEnvironment(req)
}
