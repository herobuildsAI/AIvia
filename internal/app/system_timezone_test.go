package app

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestCollectSystemIgnoresProcessTimezone(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("Unix system timezone source")
	}
	data, err := os.ReadFile("/etc/localtime")
	if err != nil {
		t.Skipf("system timezone unavailable: %v", err)
	}
	loc, err := time.LoadLocationFromTZData("system", data)
	if err != nil {
		t.Fatal(err)
	}
	_, expected := time.Now().In(loc).Zone()
	previous := time.Local
	t.Cleanup(func() { time.Local = previous })
	time.Local = time.FixedZone("stale-process-zone", expected+3600)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // No platform commands are needed to check the timezone source.
	got := CollectSystem(ctx)
	if got.OffsetMinutes == nil || *got.OffsetMinutes != expected/60 {
		t.Fatalf("system offset must ignore the stale process zone: got %+v, expected %d", got, expected/60)
	}
}

// A minimal TZif v1 file with one fixed-offset zone, independent of host tzdata.
func timezoneFixture(offset int32, name string) []byte {
	data := append([]byte("TZif\x00"), make([]byte, 15)...)
	for _, count := range []uint32{0, 0, 0, 0, 1, uint32(len(name) + 1)} {
		data = binary.BigEndian.AppendUint32(data, count)
	}
	data = binary.BigEndian.AppendUint32(data, uint32(offset))
	data = append(data, 0, 0)
	return append(append(data, name...), 0)
}

func TestSystemTimezoneReloadsChangedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "localtime")
	for _, tc := range []struct {
		name   string
		offset int
	}{{"UTC", 0}, {"IST", 330}, {"NST", -210}} {
		if err := os.WriteFile(path, timezoneFixture(int32(tc.offset*60), tc.name), 0600); err != nil {
			t.Fatal(err)
		}
		zone, offset := readSystemTimezone(path, scoreTime)
		if zone != tc.name || offset == nil || *offset != tc.offset {
			t.Fatalf("changed timezone was not reloaded: zone=%q, offset=%v, want %s/%d", zone, offset, tc.name, tc.offset)
		}
	}
}

func TestUnreadableSystemTimezoneRemainsUnknown(t *testing.T) {
	dir := t.TempDir()
	invalid := filepath.Join(dir, "invalid")
	if err := os.WriteFile(invalid, []byte("not timezone data"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(dir, "missing"), invalid, dir} {
		zone, offset := readSystemTimezone(path, scoreTime)
		if zone != "Unknown" || offset != nil {
			t.Fatalf("failed timezone read became an observation: zone=%q offset=%v", zone, offset)
		}
	}
}

func TestWindowsTimezoneRequiresCurrentOffset(t *testing.T) {
	for _, tc := range []struct {
		raw    string
		offset *int
	}{
		{`{"Timezone":"UTC","OffsetMinutes":0}`, ptr(0)},
		{`{"Timezone":"India Standard Time","OffsetMinutes":330}`, ptr(330)},
		{`{"Timezone":"Newfoundland Standard Time","OffsetMinutes":-210}`, ptr(-210)},
		{`{"Timezone":"UTC"}`, nil},
		{`{"Timezone":"UTC","OffsetMinutes":null}`, nil},
		{`{"Timezone":"UTC","OffsetMinutes":900}`, nil},
		{`{"Timezone":"UTC","OffsetMinutes":"0"}`, nil},
		{`{"OffsetMinutes":0}`, nil},
		{`invalid`, nil},
	} {
		s := SystemEvidence{Timezone: "old zone", OffsetMinutes: ptr(60)}
		applyWindowsSystem(tc.raw, &s)
		if tc.offset == nil {
			if s.OffsetMinutes != nil || s.Timezone != "Unknown" {
				t.Fatalf("invalid/missing current offset retained stale evidence: %+v", s)
			}
			f := finding(t, Score(Evidence{System: s, Browser: BrowserEvidence{OffsetMinutes: ptr(0)}}, Policy{Mode: "unknown"}, scoreTime), "timezone-system")
			if f.State != "unknown" || f.Lower != 0 || f.Upper != 3 {
				t.Fatalf("unavailable offset resolved the score: %+v", f)
			}
		} else if s.OffsetMinutes == nil || *s.OffsetMinutes != *tc.offset || s.Timezone == "Unknown" {
			t.Fatalf("current Windows timezone not applied: %+v", s)
		}
	}
}
