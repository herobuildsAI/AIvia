package app

import (
	"errors"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"unicode"
)

const defaultIPEndpoint = "https://api.ipapi.is/"

// IPSettings is persisted only in the private local store; API responses must omit APIKey.
type IPSettings struct {
	Provider string `json:"provider"`
	Endpoint string `json:"endpoint"`
	APIKey   string `json:"apiKey"`
	Revision int    `json:"revision"`
}

func NormalizeIPSettings(v IPSettings) (IPSettings, error) {
	v.Provider = strings.ToLower(strings.TrimSpace(v.Provider))
	if v.Provider == "" {
		v.Provider = "ipapi"
	}
	if v.Provider != "ipapi" && v.Provider != "custom" {
		return IPSettings{}, errors.New("Choose ipapi.is or a compatible custom IP provider.")
	}
	if v.Revision < 0 || len(v.APIKey) > 4096 || strings.ContainsFunc(v.APIKey, unicode.IsControl) {
		return IPSettings{}, errors.New("Invalid IP settings revision or API key. Keys are limited to 4 KiB without control characters.")
	}
	v.APIKey = strings.TrimSpace(v.APIKey)
	v.Endpoint = strings.TrimSpace(v.Endpoint)
	if v.Provider == "ipapi" && v.Endpoint == "" {
		v.Endpoint = defaultIPEndpoint
	}
	u, err := url.Parse(v.Endpoint)
	badEndpoint := errors.New("Use public HTTPS port 443 or a literal loopback HTTP(S) endpoint, without credentials, queries, or fragments.")
	if err != nil || len(v.Endpoint) > 2048 || u.Opaque != "" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || strings.Contains(v.Endpoint, "#") || strings.ContainsAny(u.Host, "\\%\r\n\t ") || strings.HasSuffix(u.Host, ":") {
		return IPSettings{}, badEndpoint
	}
	u.Scheme = strings.ToLower(u.Scheme)
	if u.Scheme != "http" && u.Scheme != "https" {
		return IPSettings{}, badEndpoint
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	a, ipErr := netip.ParseAddr(host)
	loopback := ipErr == nil && a.Zone() == "" && a.Unmap().IsLoopback()
	if ipErr == nil {
		if a.Zone() != "" || (!loopback && !publicIP(host)) {
			return IPSettings{}, badEndpoint
		}
		host = a.Unmap().String()
	} else {
		if len(host) > 253 || host == "localhost" || strings.HasSuffix(host, ".localhost") {
			return IPSettings{}, badEndpoint
		}
		for _, label := range strings.Split(host, ".") {
			if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' || strings.ContainsFunc(label, func(r rune) bool { return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') }) {
				return IPSettings{}, badEndpoint
			}
		}
	}
	port := u.Port()
	if port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return IPSettings{}, badEndpoint
		}
		port = strconv.Itoa(n)
	}
	if !loopback && (u.Scheme != "https" || (port != "" && port != "443")) {
		return IPSettings{}, badEndpoint
	}
	if u.Scheme == "https" && port == "443" || u.Scheme == "http" && port == "80" {
		port = ""
	}
	u.Host = host
	if strings.Contains(host, ":") {
		u.Host = "[" + host + "]"
	}
	if port != "" {
		u.Host = net.JoinHostPort(host, port)
	}
	if u.Path == "" {
		u.Path = "/"
	}
	v.Endpoint = u.String()
	if v.Provider == "ipapi" && v.Endpoint != defaultIPEndpoint {
		return IPSettings{}, errors.New("The built-in provider uses its fixed ipapi.is endpoint. Choose custom for another destination.")
	}
	return v, nil
}

func effectiveIPSettings(v IPSettings, environmentKey string) (IPSettings, error) {
	v, err := NormalizeIPSettings(v)
	if err == nil && v.Provider == "ipapi" && v.APIKey == "" {
		v.APIKey = environmentKey
		v, err = NormalizeIPSettings(v)
	}
	return v, err
}

func (s *Store) SaveIPSettings(v IPSettings) error {
	v, err := NormalizeIPSettings(v)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if v.Revision != s.state.IPSettings.Revision {
		return ErrConflict
	}
	v.Revision++
	next := cloneState(s.state)
	next.IPSettings = v
	return s.persist(next)
}
