package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStoreRoundTripAndRevision(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.SaveProfile(Profile{Name: "Muse 世界", Kind: "app", Status: "investigating", Issues: []Issue{{Stage: "registration", Error: "Verification unavailable"}}})
	if err != nil || p.ID == "" || p.Revision != 1 {
		t.Fatalf("save: %#v %v", p, err)
	}
	if _, err = s.SaveProfile(Profile{ID: p.ID, Name: "Old edit", Kind: "app", Status: "blocked"}); err == nil {
		t.Fatal("accepted stale revision")
	}
	if _, err = OpenStore(dir); err == nil {
		t.Fatal("accepted second writer")
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got := s.Snapshot().Profiles
	if len(got) != 1 || got[0].Name != "Muse 世界" || len(got[0].Issues) != 1 {
		t.Fatalf("lost data: %#v", got)
	}
	if err = s.DeleteProfile(p.ID, 0); err == nil {
		t.Fatal("accepted stale delete")
	}
	if err = s.DeleteProfile(p.ID, 1); err != nil {
		t.Fatal(err)
	}
	if len(s.Snapshot().Profiles) != 0 {
		t.Fatal("profile not deleted")
	}
}
func TestStoreCorruptionPreserved(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "store.json")
	raw := []byte(`{"version":1,"profiles":broken}`)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenStore(dir); err == nil {
		t.Fatal("accepted corrupt store")
	}
	b, _ := os.ReadFile(path)
	if string(b) != string(raw) {
		t.Fatal("overwrote corrupt data")
	}
	if _, err := os.Stat(filepath.Join(dir, "store.lock")); !os.IsNotExist(err) {
		t.Fatal("failed open leaked lock")
	}
}
func TestProfileValidation(t *testing.T) {
	s, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, origin := range []string{"http://example.com", "https://u:p@example.com", "https://example.com/?token=secret", "https://example.com/path", "https://example.com/#token", "https://example.com:8080"} {
		if _, err := s.SaveProfile(Profile{Name: "Test", Kind: "website", Status: "unchecked", Origin: origin}); err == nil {
			t.Errorf("accepted %s", origin)
		}
	}
	p, err := s.SaveProfile(Profile{Name: "<script>literal</script>", Kind: "website", Status: "unchecked", Origin: "https://example.com/"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Origin != "https://example.com" {
		t.Fatalf("origin %s", p.Origin)
	}
	p.Name = strings.Repeat("a", 161)
	if _, err := s.SaveProfile(p); err == nil {
		t.Fatal("accepted long name")
	}
}
func TestStoreFailedSaveDoesNotMutate(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p, err := s.SaveProfile(Profile{Name: "Before", Kind: "app", Status: "unchecked"})
	if err != nil {
		t.Fatal(err)
	}
	// A directory at the fixed temporary path forces persistence to fail.
	if err = os.Mkdir(filepath.Join(dir, "store.json.tmp"), 0700); err != nil {
		t.Fatal(err)
	}
	p.Name = "After"
	if _, err = s.SaveProfile(p); err == nil {
		t.Fatal("reported failed write as saved")
	}
	if s.Snapshot().Profiles[0].Name != "Before" {
		t.Fatal("failed save changed memory")
	}
}

func TestStoreRejectsEmptyDocumentsWithoutReplacingBackup(t *testing.T) {
	for _, payload := range []string{"null", "{}", `{"version":1}`, `{"version":1,"profiles":null,"reports":[]}`} {
		t.Run(payload, func(t *testing.T) {
			dir := t.TempDir()
			original := filepath.Join(dir, "store.json")
			backup := original + ".bak"
			if err := os.WriteFile(original, []byte(payload), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(backup, []byte("previous recovery data"), 0600); err != nil {
				t.Fatal(err)
			}
			s, err := OpenStore(dir)
			if err == nil {
				s.Close()
				t.Fatal("malformed existing store was accepted as an empty workspace")
			}
			b, _ := os.ReadFile(original)
			if string(b) != payload {
				t.Fatal("malformed store was modified")
			}
			b, _ = os.ReadFile(backup)
			if string(b) != "previous recovery data" {
				t.Fatal("recovery data was modified")
			}
		})
	}
}
