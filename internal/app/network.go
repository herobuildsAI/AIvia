package app

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

func itoa(n int) string { return strconv.Itoa(n) }

var reservedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"), netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"), netip.MustParsePrefix("198.18.0.0/15"), netip.MustParsePrefix("198.51.100.0/24"), netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("240.0.0.0/4"), netip.MustParsePrefix("2001:db8::/32"), netip.MustParsePrefix("2001::/32"), netip.MustParsePrefix("2002::/16"), netip.MustParsePrefix("64:ff9b::/96"), netip.MustParsePrefix("64:ff9b:1::/48"),
}

func publicIP(raw string) bool {
	a, err := netip.ParseAddr(raw)
	if err != nil || a.Zone() != "" {
		return false
	}
	a = a.Unmap()
	if !a.IsGlobalUnicast() || a.IsPrivate() || a.IsLoopback() || a.IsLinkLocalUnicast() {
		return false
	}
	for _, p := range reservedPrefixes {
		if p.Contains(a) {
			return false
		}
	}
	return true
}

// canonicalIP is also the identity used for scoring and lookup deduplication.
func canonicalIP(raw string) string {
	if !publicIP(raw) {
		return ""
	}
	a, _ := netip.ParseAddr(raw)
	return a.Unmap().String()
}
func familyOf(raw string) string {
	a, err := netip.ParseAddr(raw)
	if err != nil {
		return ""
	}
	if a.Unmap().Is4() {
		return "ipv4"
	}
	return "ipv6"
}
func fetch(ctx context.Context, client *http.Client, method, endpoint string, body []byte, limit int64) ([]byte, http.Header, error) {
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, nil, errors.New("Invalid request destination.")
	}
	req.Header.Set("User-Agent", "AIvia/0.1")
	req.Header.Set("Cache-Control", "no-cache")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, nil, errors.New("Request unavailable, canceled, or timed out.")
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, res.Header, fmt.Errorf("Remote HTTP status %d. No retries were made.", res.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil || int64(len(b)) > limit {
		return nil, res.Header, errors.New("Response was unreadable or too large.")
	}
	return b, res.Header, nil
}
func networkClient(opts Options) (*http.Client, error) {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.MaxConnsPerHost = 4
	tr.ResponseHeaderTimeout = 8 * time.Second
	tr.Proxy = validatedProxyFromEnvironment
	if opts.Proxy != "" {
		u, err := parseProxyURL(opts.Proxy, false)
		if err != nil {
			return nil, err
		}
		tr.Proxy = http.ProxyURL(u)
	}
	return &http.Client{Transport: tr, Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, nil
}
func parseIntelligence(b []byte, ip string) (*Intelligence, error) {
	var d struct {
		IP         string `json:"ip"`
		Error      string `json:"error"`
		Abuse      *bool  `json:"is_abuser"`
		Tor        *bool  `json:"is_tor"`
		Proxy      *bool  `json:"is_proxy"`
		VPN        *bool  `json:"is_vpn"`
		Datacenter *bool  `json:"is_datacenter"`
		Location   struct {
			Country  string `json:"country_code"`
			Region   string `json:"state"`
			Timezone string `json:"timezone"`
		} `json:"location"`
		ASN     json.RawMessage `json:"asn"`
		Company json.RawMessage `json:"company"`
	}
	if err := json.Unmarshal(b, &d); err != nil || canonicalIP(d.IP) == "" || canonicalIP(d.IP) != canonicalIP(ip) || d.Error != "" {
		return nil, errors.New("IP provider returned invalid or mismatched data.")
	}
	if d.Location.Country != "" && !validCountry(d.Location.Country) {
		return nil, errors.New("IP provider returned an unknown country code.")
	}
	if len(d.Location.Region) > 160 || len(d.Location.Timezone) > 160 {
		return nil, errors.New("IP provider returned oversized fields.")
	}
	v := &Intelligence{Country: d.Location.Country, Region: d.Location.Region, Timezone: d.Location.Timezone, Abuse: d.Abuse, Tor: d.Tor, Proxy: d.Proxy, VPN: d.VPN, Datacenter: d.Datacenter}
	// Optional network metadata cannot invalidate otherwise usable country/flag evidence.
	var asn, company map[string]json.RawMessage
	_ = json.Unmarshal(d.ASN, &asn)
	_ = json.Unmarshal(d.Company, &company)
	var number uint32
	if json.Unmarshal(asn["asn"], &number) == nil && number > 0 {
		v.ASN = strconv.FormatUint(uint64(number), 10)
	}
	v.ASNOrganization, v.ASNType = metadataText(asn["org"]), metadataText(asn["type"])
	v.CompanyName, v.CompanyType = metadataText(company["name"]), metadataText(company["type"])
	v.ASNRoute = matchingNetwork(metadataText(asn["route"]), ip, false)
	v.CompanyNetwork = matchingNetwork(metadataText(company["network"]), ip, true)
	if v.ASNRoute != "" {
		v.ASNAbuseRatio = abuseRatio(asn["abuser_score"])
	}
	if v.CompanyNetwork != "" {
		v.CompanyAbuseRatio = abuseRatio(company["abuser_score"])
	}
	return v, nil
}
func metadataText(raw json.RawMessage) string {
	var value string
	if json.Unmarshal(raw, &value) != nil || len(value) > 160 {
		return ""
	}
	return strings.TrimSpace(value)
}
func abuseRatio(raw json.RawMessage) *float64 {
	value := metadataText(raw)
	// ipapi.is appends a descriptive label. Only its numeric proportion is evidence.
	if start := strings.Index(value, " ("); start >= 0 {
		if !strings.HasSuffix(value, ")") {
			return nil
		}
		value = value[:start]
	}
	ratio, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(ratio) || math.IsInf(ratio, 0) || ratio < 0 || ratio > 1 {
		return nil
	}
	return &ratio
}
func matchingNetwork(raw, ip string, allowRange bool) string {
	address, err := netip.ParseAddr(canonicalIP(ip))
	if err != nil {
		return ""
	}
	if prefix, err := netip.ParsePrefix(raw); err == nil {
		if prefix != prefix.Masked() || prefix.Addr().Is4() != address.Is4() || !prefix.Contains(address) {
			return ""
		}
		return prefix.String()
	}
	if !allowRange {
		return ""
	}
	start, end, ok := strings.Cut(raw, " - ")
	if !ok {
		return ""
	}
	first, errFirst := netip.ParseAddr(strings.TrimSpace(start))
	last, errLast := netip.ParseAddr(strings.TrimSpace(end))
	if errFirst != nil || errLast != nil || first.Zone() != "" || last.Zone() != "" {
		return ""
	}
	first, last = first.Unmap(), last.Unmap()
	if first.Is4() != address.Is4() || last.Is4() != address.Is4() || first.Compare(last) > 0 || first.Compare(address) > 0 || last.Compare(address) < 0 {
		return ""
	}
	return first.String() + " - " + last.String()
}
func CollectNetwork(ctx context.Context, in RunInput, opts Options) Evidence {
	e := Evidence{Browser: in.Browser, System: CollectSystem(ctx), Network: in.Network, WebRTCChecked: in.WebRTC, Exits: []Exit{}, Warnings: []string{}, Target: Probe{State: "unknown", Summary: "Service reachability was not checked."}}
	if !in.Network {
		return e
	}
	// Client-supplied evidence is bounded/validated by the HTTP layer; do not accept supplied intelligence.
	for _, x := range in.Exits {
		x.Intel = nil
		x.IP = canonicalIP(x.IP)
		e.Exits = append(e.Exits, x)
	}
	client, err := networkClient(opts)
	if err != nil {
		e.Warnings = append(e.Warnings, err.Error())
		return e
	}
	defer client.CloseIdleConnections()
	type result struct {
		x    Exit
		date time.Time
		mid  time.Time
	}
	ch := make(chan result, 2)
	var wg sync.WaitGroup
	for _, v := range []struct{ family, url string }{{"ipv4", "https://api.ipify.org?format=json"}, {"ipv6", "https://api6.ipify.org?format=json"}} {
		wg.Add(1)
		go func(family, endpoint string) {
			defer wg.Done()
			start := time.Now()
			b, h, err := fetch(ctx, client, http.MethodGet, endpoint, nil, 4096)
			end := time.Now()
			x := Exit{Path: "agent", Family: family}
			var d struct {
				IP string `json:"ip"`
			}
			if err != nil {
				x.Error = err.Error()
			} else if json.Unmarshal(b, &d) != nil || !publicIP(d.IP) || familyOf(d.IP) != family {
				x.Error = "Public egress response was invalid."
			} else {
				x.IP = canonicalIP(d.IP)
			}
			date, _ := http.ParseTime(h.Get("Date"))
			if end.Sub(start) > 10*time.Second {
				date = time.Time{}
			}
			ch <- result{x, date, start.Add(end.Sub(start) / 2)}
		}(v.family, v.url)
	}
	wg.Wait()
	close(ch)
	var date1, mid1 time.Time
	for r := range ch {
		e.Exits = append(e.Exits, r.x)
		if r.x.Family == "ipv4" {
			date1 = r.date
			mid1 = r.mid
		}
	}
	start := time.Now()
	_, header, clockErr := fetch(ctx, client, http.MethodHead, "https://www.cloudflare.com/", nil, 1024)
	end := time.Now()
	date2, dateErr := http.ParseTime(header.Get("Date"))
	if clockErr == nil && dateErr == nil && !date1.IsZero() && end.Sub(start) < 10*time.Second {
		a := date1.Sub(mid1).Seconds()
		b := date2.Sub(start.Add(end.Sub(start) / 2)).Seconds()
		if math.Abs(a-b) < 10 {
			v := (a + b) / 2
			e.ClockSkewSeconds = &v
		}
	}
	if in.Intelligence {
		collectIPIntelligence(ctx, &e, opts, client)
	}
	if in.Target {
		e.Target = probeTarget(ctx, opts.TargetOrigin, opts)
	}
	return e
}

func collectIPIntelligence(ctx context.Context, e *Evidence, opts Options, client *http.Client) {
	settings, err := effectiveIPSettings(opts.IPSettings, opts.IPKey)
	if err != nil {
		e.Warnings = append(e.Warnings, err.Error())
		return
	}
	e.IPSource = "https://ipapi.is/developers.html"
	if settings.Provider == "custom" {
		e.IPSource = "User-configured IP intelligence provider"
		client, err = customIPClient(settings, opts)
		if err != nil {
			e.Warnings = append(e.Warnings, err.Error())
			return
		}
		defer client.CloseIdleConnections()
	} else if settings.APIKey == "" {
		e.Warnings = append(e.Warnings, "IP intelligence is not configured. Add your own API key in Settings or AIVPN_IPAPI_KEY.")
		return
	}
	cache := map[string]*Intelligence{}
	queried := map[string]bool{}
	for i := range e.Exits {
		ip := canonicalIP(e.Exits[i].IP)
		if ip == "" {
			continue
		}
		if !queried[ip] {
			if len(queried) >= 8 {
				e.Warnings = append(e.Warnings, "IP lookup limit reached; remaining addresses are unknown.")
				continue
			}
			queried[ip] = true
			payload, _ := json.Marshal(map[string]string{"q": ip, "key": settings.APIKey})
			b, _, err := fetch(ctx, client, http.MethodPost, settings.Endpoint, payload, 64<<10)
			if err != nil {
				e.Warnings = append(e.Warnings, "IP lookup stopped: "+err.Error())
				break
			}
			v, err := parseIntelligence(b, ip)
			if err != nil {
				e.Warnings = append(e.Warnings, err.Error())
			} else {
				cache[ip] = v
			}
		}
		e.Exits[i].Intel = cache[ip]
	}
}

func customIPClient(settings IPSettings, opts Options) (*http.Client, error) {
	u, _ := url.Parse(settings.Endpoint) // The caller supplies normalized settings.
	req := &http.Request{URL: u}
	proxy, err := validatedProxyFromEnvironment(req)
	if opts.Proxy != "" || proxy != nil || err != nil {
		return nil, errors.New("Custom IP lookup is unavailable through this proxy route; no direct fallback was attempted.")
	}
	host, port := u.Hostname(), u.Port()
	if port == "" {
		port = "443"
		if u.Scheme == "http" {
			port = "80"
		}
	}
	loopback := net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback()
	tr := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}, MaxConnsPerHost: 4, ResponseHeaderTimeout: 8 * time.Second}
	tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if addr != net.JoinHostPort(host, port) {
			return nil, errors.New("IP provider destination changed.")
		}
		if loopback {
			var d net.Dialer
			return d.DialContext(ctx, network, net.JoinHostPort(host, port))
		}
		return publicDial(ctx, network, addr)
	}
	return &http.Client{Transport: tr, Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, nil
}

func publicAddresses(ips []net.IPAddr) bool {
	if len(ips) == 0 {
		return false
	}
	for _, ip := range ips {
		if ip.Zone != "" || !publicIP(ip.IP.String()) {
			return false
		}
	}
	return true
}

func publicDial(ctx context.Context, network, addr string) (net.Conn, error) {
	// Transports may detach a dial from the request deadline for connection reuse.
	// Bound DNS and all connection attempts even in that case.
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	host, port, err := net.SplitHostPort(addr)
	if err != nil || port != "443" {
		return nil, errors.New("Only public HTTPS port 443 is allowed.")
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil || len(ips) == 0 {
		return nil, errors.New("Target DNS lookup failed.")
	}
	if !publicAddresses(ips) {
		return nil, errors.New("Target resolves to a non-public address.")
	}
	var d net.Dialer
	return dialPublicAddresses(ctx, network, port, ips, d.DialContext)
}

func dialPublicAddresses(ctx context.Context, network, port string, ips []net.IPAddr, dial func(context.Context, string, string) (net.Conn, error)) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	for i, ip := range ips {
		if ctx.Err() != nil {
			break
		}
		// Reserve time for every remaining validated address; one blackhole must
		// not consume the budget of an otherwise reachable destination.
		budget := time.Until(deadline) / time.Duration(len(ips)-i)
		attempt, stop := context.WithTimeout(ctx, budget)
		c, e := dial(attempt, network, net.JoinHostPort(ip.IP.String(), port))
		stop()
		if e == nil {
			return c, nil
		}
		if ctx.Err() != nil {
			break
		}
	}
	return nil, errors.New("Target connection failed.")
}
func probeTarget(ctx context.Context, origin string, opts Options) Probe {
	unknown := func(s string) Probe { return Probe{State: "unknown", Summary: s} }
	normalized, err := validateOrigin(origin)
	if err != nil || normalized == "" {
		return unknown("Add a public HTTPS origin to this service before checking reachability.")
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodHead, normalized, nil)
	proxy, proxyErr := validatedProxyFromEnvironment(req)
	if opts.Proxy != "" || proxy != nil || proxyErr != nil {
		return unknown("Target check is unavailable through this proxy route; no direct fallback was attempted.")
	}
	if ip := net.ParseIP(req.URL.Hostname()); ip != nil && !publicIP(ip.String()) {
		return unknown("Private or reserved target destinations are not allowed.")
	}
	client := &http.Client{Timeout: 8 * time.Second, Transport: &http.Transport{DialContext: publicDial, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}, ResponseHeaderTimeout: 8 * time.Second}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	start := time.Now()
	res, err := client.Do(req)
	if err != nil {
		return unknown("Target DNS, TLS, or HTTP connection failed. This does not establish service blocking.")
	}
	defer res.Body.Close()
	return Probe{State: "observed", Status: res.StatusCode, Milliseconds: time.Since(start).Milliseconds(), Summary: "Public HTTPS response received. HTTP status does not establish registration, login, or account eligibility. Redirects were not followed."}
}
