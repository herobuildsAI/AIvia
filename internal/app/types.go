package app

import "time"

type Policy struct {
	Mode            string              `json:"mode"`
	Builtin         string              `json:"builtin,omitempty"`
	Countries       []string            `json:"countries,omitempty"`
	Source          string              `json:"source,omitempty"`
	CheckedAt       string              `json:"checkedAt,omitempty"`
	Provenance      string              `json:"provenance,omitempty"`
	ExcludedRegions map[string][]string `json:"excludedRegions,omitempty"`
}
type Issue struct {
	ID         string `json:"id"`
	Stage      string `json:"stage"`
	Error      string `json:"error"`
	Notes      string `json:"notes"`
	ObservedAt string `json:"observedAt"`
	Resolved   bool   `json:"resolved"`
}
type Profile struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Kind      string  `json:"kind"`
	Origin    string  `json:"origin"`
	Status    string  `json:"status"`
	Notes     string  `json:"notes"`
	Issues    []Issue `json:"issues"`
	Policy    Policy  `json:"policy"`
	Revision  int     `json:"revision"`
	CreatedAt string  `json:"createdAt"`
	UpdatedAt string  `json:"updatedAt"`
}
type ModelSettings struct {
	Provider       string `json:"provider"`
	Endpoint       string `json:"endpoint"`
	Model          string `json:"model"`
	LocalConfirmed bool   `json:"localConfirmed"`
	CLIPath        string `json:"cliPath,omitempty"`
	CloudConfirmed bool   `json:"cloudConfirmed,omitempty"`
}
type State struct {
	Version    int              `json:"version"`
	Profiles   []Profile        `json:"profiles"`
	Reports    []RedactedReport `json:"reports"`
	Settings   ModelSettings    `json:"settings"`
	IPSettings IPSettings       `json:"ipSettings"`
}
type BrowserEvidence struct {
	Timezone      string   `json:"timezone"`
	OffsetMinutes *int     `json:"offsetMinutes"`
	Languages     []string `json:"languages"`
}
type SystemEvidence struct {
	OS             string   `json:"os"`
	Arch           string   `json:"arch"`
	Timezone       string   `json:"timezone"`
	OffsetMinutes  *int     `json:"offsetMinutes"`
	Locale         string   `json:"locale"`
	Proxy          string   `json:"proxy"`
	DNS            string   `json:"dns"`
	Route          string   `json:"route"`
	InterfaceCount int      `json:"interfaceCount"`
	Warnings       []string `json:"warnings"`
}
type Intelligence struct {
	Country    string `json:"country"`
	Region     string `json:"region"`
	Timezone   string `json:"timezone"`
	ASN        string `json:"asn"`
	Abuse      *bool  `json:"abuse"`
	Tor        *bool  `json:"tor"`
	Proxy      *bool  `json:"proxy"`
	VPN        *bool  `json:"vpn"`
	Datacenter *bool  `json:"datacenter"`
}
type Exit struct {
	Path   string        `json:"path"`
	Family string        `json:"family"`
	IP     string        `json:"ip,omitempty"`
	Error  string        `json:"error,omitempty"`
	Intel  *Intelligence `json:"intelligence,omitempty"`
}
type Probe struct {
	State        string `json:"state"`
	Summary      string `json:"summary"`
	Status       int    `json:"status,omitempty"`
	Milliseconds int64  `json:"milliseconds,omitempty"`
}
type Evidence struct {
	Browser          BrowserEvidence `json:"browser"`
	System           SystemEvidence  `json:"system"`
	Exits            []Exit          `json:"exits"`
	WebRTCChecked    bool            `json:"webrtcChecked"`
	Network          bool            `json:"network"`
	Target           Probe           `json:"target"`
	ClockSkewSeconds *float64        `json:"clockSkewSeconds"`
	Warnings         []string        `json:"warnings"`
	IPSource         string          `json:"ipSource,omitempty"`
}
type Finding struct {
	ID             string  `json:"id"`
	Title          string  `json:"title"`
	Weight         float64 `json:"weight"`
	Lower          float64 `json:"lower"`
	Upper          float64 `json:"upper"`
	State          string  `json:"state"`
	Explanation    string  `json:"explanation"`
	Recommendation string  `json:"recommendation"`
	Source         string  `json:"source"`
}
type RedactedReport struct {
	ID              string    `json:"id"`
	ProfileID       string    `json:"profileId"`
	ProfileRevision int       `json:"profileRevision"`
	CreatedAt       string    `json:"createdAt"`
	ScoreVersion    string    `json:"scoreVersion"`
	Policy          Policy    `json:"policy"`
	Lower           float64   `json:"lower"`
	Upper           float64   `json:"upper"`
	Coverage        float64   `json:"coverage"`
	Grade           string    `json:"grade"`
	Findings        []Finding `json:"findings"`
	OS              string    `json:"os"`
	Arch            string    `json:"arch"`
	Timezone        string    `json:"timezone"`
	Target          Probe     `json:"target"`
}
type Report struct {
	RedactedReport
	Evidence Evidence `json:"evidence"`
}
type RunInput struct {
	ProfileID          string          `json:"profileId"`
	Revision           int             `json:"revision"`
	Browser            BrowserEvidence `json:"browser"`
	Exits              []Exit          `json:"exits"`
	Network            bool            `json:"network"`
	Intelligence       bool            `json:"intelligence"`
	IPSettingsRevision int             `json:"ipSettingsRevision"`
	WebRTC             bool            `json:"webrtc"`
	Target             bool            `json:"target"`
}
type Options struct {
	TargetOrigin string
	Proxy        string
	STUN         string
	IPKey        string
	IPSettings   IPSettings
	ModelToken   string
	Host         string
}
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
type Model struct {
	ID       string `json:"id"`
	Locality string `json:"locality"`
}

func timestamp() string { return time.Now().UTC().Format(time.RFC3339) }
