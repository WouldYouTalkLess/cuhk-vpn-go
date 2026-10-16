package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestPortableProfileOverridesLegacyAndKeepsStorageChoice(t *testing.T) {
	portable, legacy := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(legacy, "settings.json"), []byte(`{"username":"old-user","remember":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := load(portable, legacy)
	if err != nil || s.Username != "old-user" || s.PasswordStorage != PasswordSystem {
		t.Fatal("legacy credential mode was not preserved", err)
	}
	s.Username, s.PasswordStorage = "portable-user", PasswordPortable
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if err = WritePrivateFile(filepath.Join(portable, "settings.json"), b); err != nil {
		t.Fatal(err)
	}
	s, err = load(portable, legacy)
	if err != nil || s.Username != "portable-user" || s.PasswordStorage != PasswordPortable {
		t.Fatal("portable profile not preferred", err)
	}
	// Replacing a profile must work on Windows as well as Unix.
	s.Username = "updated-user"
	b, err = json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if err = WritePrivateFile(filepath.Join(portable, "settings.json"), b); err != nil {
		t.Fatal(err)
	}
	s, err = load(portable, legacy)
	if err != nil || s.Username != "updated-user" {
		t.Fatal("profile was not replaced", err)
	}
	if err = os.WriteFile(filepath.Join(portable, "settings.json"), []byte(`{"passwordStorage":"invalid"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = load(portable, legacy); err == nil {
		t.Fatal("corrupt portable profile silently fell back to the legacy one")
	}
}

func TestSplitValidation(t *testing.T) {
	for _, routes := range [][]string{nil, {"0.0.0.0/0"}, {"::/0"}, {"garbage"}, {"0.0.0.0/1", "128.0.0.0/1"}, {"0.0.0.0/2", "64.0.0.0/2", "128.0.0.0/1"}} {
		s := Default()
		s.CIDRs = routes
		if _, e := s.Validate(false); e == nil {
			t.Errorf("accepted routes %v", routes)
		}
	}
	s := Default()
	s.CIDRs = []string{"10.20.1.7/16", "10.20.0.0/16", "172.20.1.1/24"}
	got, e := s.Validate(false)
	if e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(got.CIDRs, []string{"10.20.0.0/16", "172.20.1.0/24"}) {
		t.Fatal(got.CIDRs)
	}
}
func TestConnectionValidation(t *testing.T) {
	for _, server := range []string{"http://vpn.example.com", "https://user:pass@vpn.example.com", "https://vpn.example.com/#a", "https://vpn.example.com?token=secret"} {
		s := Default()
		s.Server = server
		if _, e := s.Validate(false); e == nil {
			t.Fatal(server)
		}
	}
	s := Default()
	s.Username = "name\n--insecure"
	if _, e := s.Validate(true); e == nil {
		t.Fatal("accepted newline")
	}
}
