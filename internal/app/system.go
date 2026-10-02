package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

type limitedBuffer struct {
	bytes.Buffer
	limit int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > b.limit {
		return 0, errors.New("command output limit")
	}
	return b.Buffer.Write(p)
}
func command(ctx context.Context, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	var b = limitedBuffer{limit: 64 << 10}
	cmd.Stdout = &b
	cmd.Stderr = io.Discard
	cmd.WaitDelay = time.Second
	if err := cmd.Run(); err != nil {
		return "", errors.New("System query unavailable.")
	}
	return strings.TrimSpace(b.String()), nil
}

func readSystemTimezone(path string, now time.Time) (string, *int) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "Unknown", nil
	}
	loc, err := time.LoadLocationFromTZData("system", data)
	if err != nil {
		return "Unknown", nil
	}
	zone, offset := now.In(loc).Zone()
	minutes := offset / 60
	return zone, &minutes
}

func CollectSystem(ctx context.Context) SystemEvidence {
	s := SystemEvidence{OS: runtime.GOOS, Arch: runtime.GOARCH, Timezone: "Unknown", Locale: "Unknown", Proxy: "No HTTP proxy environment setting detected; application routing may differ.", DNS: "Unknown", Route: "Unknown", Warnings: []string{}}
	if runtime.GOOS == "darwin" || runtime.GOOS == "linux" {
		// Read the OS configuration on every scan; time.Local caches it and honors process TZ.
		s.Timezone, s.OffsetMinutes = readSystemTimezone("/etc/localtime", time.Now())
	}
	ifaces, err := net.Interfaces()
	if err == nil {
		for _, v := range ifaces {
			if v.Flags&net.FlagUp != 0 && v.Flags&net.FlagLoopback == 0 {
				s.InterfaceCount++
			}
		}
	}
	for _, key := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy"} {
		if os.Getenv(key) != "" {
			s.Proxy = "HTTP proxy environment setting is present; credentials are hidden."
			break
		}
	}
	if os.Getenv("ALL_PROXY") != "" || os.Getenv("all_proxy") != "" {
		s.Warnings = append(s.Warnings, "ALL_PROXY is not automatically applied. Use the explicit -proxy option if needed.")
	}
	switch runtime.GOOS {
	case "darwin":
		if v, e := command(ctx, "/usr/bin/defaults", "read", "-g", "AppleLocale"); e == nil && len(v) < 160 {
			s.Locale = v
		}
		if v, e := command(ctx, "/usr/sbin/scutil", "--proxy"); e == nil {
			types := []string{}
			for _, k := range []string{"HTTPEnable", "HTTPSEnable", "SOCKSEnable", "ProxyAutoConfigEnable", "ProxyAutoDiscoveryEnable"} {
				if strings.Contains(v, k+" : 1") {
					types = append(types, strings.TrimSuffix(k, "Enable"))
				}
			}
			if len(types) > 0 {
				s.Proxy += " System settings: " + strings.Join(types, ", ") + ". System/PAC settings are not automatically applied to Go requests."
			}
		} else {
			s.Warnings = append(s.Warnings, "System proxy configuration could not be read.")
		}
		if v, e := command(ctx, "/usr/sbin/scutil", "--dns"); e == nil {
			s.DNS = "Configured resolver entries: " + itoa(strings.Count(v, "nameserver[")) + ". This is not a DNS leak test."
		}
		if v, e := command(ctx, "/sbin/route", "-n", "get", "default"); e == nil && strings.Contains(v, "gateway:") {
			s.Route = "A default route is configured; individual applications can use different routes."
		}
	case "windows":
		script := `$z=Get-TimeZone -ErrorAction Stop; $p=Get-ItemProperty 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Internet Settings' -ErrorAction SilentlyContinue; [pscustomobject]@{Locale=(Get-Culture).Name; Timezone=$z.Id; OffsetMinutes=[int]($z.GetUtcOffset([DateTimeOffset]::UtcNow).TotalMinutes); Proxy=[bool]$p.ProxyEnable; PAC=[bool]$p.AutoConfigURL; DNS=@(Get-DnsClientServerAddress -ErrorAction SilentlyContinue | Where-Object {$_.ServerAddresses.Count -gt 0}).Count; Routes=@(Get-NetRoute -DestinationPrefix '0.0.0.0/0' -ErrorAction SilentlyContinue).Count} | ConvertTo-Json -Compress`
		if v, e := command(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script); e == nil {
			applyWindowsSystem(v, &s)
		} else {
			s.Warnings = append(s.Warnings, "Windows network configuration queries were unavailable or timed out.")
		}
	case "linux":
		if v, e := command(ctx, "locale"); e == nil {
			for _, line := range strings.Split(v, "\n") {
				if strings.HasPrefix(line, "LANG=") && len(line) < 160 {
					s.Locale = strings.Trim(strings.TrimPrefix(line, "LANG="), "\"")
				}
			}
		}
		if link, e := os.Readlink("/etc/localtime"); e == nil && s.OffsetMinutes != nil {
			if _, after, ok := strings.Cut(link, "zoneinfo/"); ok && len(after) < 160 {
				s.Timezone = after
			}
		}
		if f, e := os.Open("/etc/resolv.conf"); e == nil {
			b, _ := io.ReadAll(io.LimitReader(f, 65536))
			f.Close()
			s.DNS = "Configured resolver entries: " + itoa(strings.Count(string(b), "nameserver ")) + ". Browser secure DNS may differ."
		}
		if v, e := command(ctx, "ip", "route", "show", "default"); e == nil && v != "" {
			s.Route = "A default route is configured."
		}
		s.Warnings = append(s.Warnings, "Desktop-specific proxy settings are not collected on Linux.")
	}
	if s.OffsetMinutes == nil {
		s.Warnings = append(s.Warnings, "Current system timezone offset could not be read; timezone comparison remains unknown.")
	}
	return s
}

func applyWindowsSystem(raw string, s *SystemEvidence) {
	s.Timezone, s.OffsetMinutes = "Unknown", nil
	var d struct {
		Locale, Timezone string
		OffsetMinutes    *int
		Proxy, PAC       bool
		DNS, Routes      int
	}
	if json.Unmarshal([]byte(raw), &d) != nil {
		return
	}
	if len(d.Locale) < 160 {
		s.Locale = d.Locale
	}
	if d.Timezone != "" && len(d.Timezone) < 160 && d.OffsetMinutes != nil && *d.OffsetMinutes >= -840 && *d.OffsetMinutes <= 840 {
		s.Timezone, s.OffsetMinutes = d.Timezone, d.OffsetMinutes
	}
	if d.Proxy || d.PAC {
		s.Proxy += " Windows user proxy/PAC is configured; WinHTTP and application settings may differ."
	}
	s.DNS = "Configured DNS interfaces: " + itoa(d.DNS) + ". External DNS leakage was not tested."
	if d.Routes > 0 {
		s.Route = "An IPv4 default route is configured."
	}
}
