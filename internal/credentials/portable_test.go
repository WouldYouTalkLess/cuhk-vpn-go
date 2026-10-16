package credentials

import (
	"bytes"
	"cuhk-vpn-go/internal/config"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func copyDirectory(t *testing.T, from, to string) {
	t.Helper()
	err := filepath.WalkDir(from, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(from, path)
		if err != nil {
			return err
		}
		dest := filepath.Join(to, rel)
		if entry.IsDir() {
			return os.MkdirAll(dest, 0700)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(dest, b, 0600)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestPortableVaultSurvivesDirectoryCopy(t *testing.T) {
	s := FileStore{Directory: t.TempDir()}
	account, password := "https://vpn.example.edu|student", "test-only-密码-78!"
	if err := s.Set(account, password); err != nil {
		t.Fatal(err)
	}
	profile := config.Default()
	profile.Username, profile.Remember = "student", true
	b, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	if err = config.WritePrivateFile(filepath.Join(s.Directory, "settings.json"), b); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{s.path(account), filepath.Join(s.Directory, "settings.json")} {
		b, err = os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(b, []byte(password)) {
			t.Fatal("plaintext password written to disk")
		}
	}
	dest := FileStore{Directory: t.TempDir()}
	copyDirectory(t, s.Directory, dest.Directory)
	got, err := dest.Get(account)
	if err != nil || got != password {
		t.Fatal("copied vault could not be decrypted", err)
	}
	if err = dest.Set(account, "updated-test-password"); err != nil {
		t.Fatal(err)
	}
	got, err = s.Get(account)
	if err != nil || got != password {
		t.Fatal("copy changed the original vault", err)
	}
}

func TestPortableVaultRejectsTamperingAndAccountSwap(t *testing.T) {
	for _, change := range []string{"ciphertext", "key", "account", "header", "truncated"} {
		t.Run(change, func(t *testing.T) {
			s := FileStore{Directory: t.TempDir()}
			account := "vpn|account-one"
			if err := s.Set(account, "test-password"); err != nil {
				t.Fatal(err)
			}
			path := s.path(account)
			if change == "key" {
				path = filepath.Join(s.directory(), "key.bin")
			}
			b, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			switch change {
			case "account":
				account = "vpn|account-two"
				path = s.path(account)
			case "header":
				b[0] ^= 1
			case "truncated":
				b = b[:len(header)+3]
			default:
				b[len(b)-1] ^= 1
			}
			if err = os.WriteFile(path, b, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err = s.Get(account); !errors.Is(err, errInvalidVault) {
				t.Fatalf("accepted %s: %v", change, err)
			}
		})
	}
}

func TestPortableVaultMissingKeyDoesNotDestroyOtherAccounts(t *testing.T) {
	s := FileStore{Directory: t.TempDir()}
	if err := s.Set("one", "test-password"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.directory(), "key.bin")
	key, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Get("one"); err == nil {
		t.Fatal("accepted missing key")
	}
	if err = s.Set("two", "another-password"); err == nil {
		t.Fatal("generated replacement key for an existing vault")
	}
	if err = os.WriteFile(path, key, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Get("one"); err != nil {
		t.Fatal("original key could not recover original account", err)
	}
}

func TestPortableVaultFreshNoncesAndSelectedDeletion(t *testing.T) {
	s := FileStore{Directory: t.TempDir()}
	if _, err := s.Get("one"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if err := s.Set("one", "test-password"); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(s.path("one"))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Set("one", "test-password"); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(s.path("one"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(first, second) {
		t.Fatal("reused ciphertext nonce")
	}
	if err = s.Set("two", "other-password"); err != nil {
		t.Fatal(err)
	}
	if err = s.Delete("one"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Get("one"); !errors.Is(err, ErrNotFound) {
		t.Fatal("deleted password still present", err)
	}
	if _, err = s.Get("two"); err != nil {
		t.Fatal("deleting one account damaged another", err)
	}
}
