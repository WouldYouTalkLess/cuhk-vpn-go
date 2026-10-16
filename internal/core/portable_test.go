package core

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func digest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func TestDownloadHashCacheAndFailure(t *testing.T) {
	data := []byte("verified payload")
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests++; w.Write(data) }))
	defer server.Close()
	m := Manager{Base: t.TempDir(), Client: server.Client(), AllowHTTP: true}
	s := Source{Name: "fixture", URL: server.URL, SHA256: digest(data)}
	p, e := m.download(context.Background(), s)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = m.download(context.Background(), s); e != nil || requests != 1 {
		t.Fatalf("cache err=%v requests=%d", e, requests)
	}
	if e = os.WriteFile(p, []byte("corrupt"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = m.download(context.Background(), s); e != nil || requests != 2 {
		t.Fatalf("corrupt cache not repaired: %v", e)
	}
	s.SHA256 = strings.Repeat("00", 32)
	if _, e = m.download(context.Background(), s); e == nil {
		t.Fatal("accepted hash mismatch")
	}
	if _, e = os.Stat(filepath.Join(m.Base, "cores", ".cache", s.SHA256)); !os.IsNotExist(e) {
		t.Fatal("published unverified download")
	}
}
func TestDownloadCancellationAndHTTPS(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("partial"))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	m := Manager{Base: t.TempDir(), Client: server.Client(), AllowHTTP: true}
	s := Source{Name: "fixture", URL: server.URL, SHA256: strings.Repeat("00", 32)}
	ctx, c := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer c()
	if _, e := m.download(ctx, s); e == nil {
		t.Fatal("accepted interrupted download")
	}
	entries, _ := os.ReadDir(filepath.Join(m.Base, "cores", ".cache"))
	if len(entries) != 0 {
		t.Fatal("partial download left in cache")
	}
	m.AllowHTTP = false
	if _, e := m.download(context.Background(), s); e == nil {
		t.Fatal("accepted plain HTTP")
	}
}
func tarBytes(t *testing.T, entries []*tar.Header, payload []string) []byte {
	t.Helper()
	var b bytes.Buffer
	w := tar.NewWriter(&b)
	for i, h := range entries {
		if e := w.WriteHeader(h); e != nil {
			t.Fatal(e)
		}
		if h.Size > 0 {
			w.Write([]byte(payload[i]))
		}
	}
	w.Close()
	return b.Bytes()
}
func TestUnsafeArchivePathsAndLinks(t *testing.T) {
	for _, name := range []string{"../escape", "a/../../escape", "/tmp/escape", "C:\\escape", "\\\\server\\share\\escape"} {
		if _, e := safePath(t.TempDir(), name); e == nil {
			t.Errorf("accepted %s", name)
		}
	}
	d := t.TempDir()
	u := unpacker{root: d}
	b := tarBytes(t, []*tar.Header{{Name: "lib/bad.so", Typeflag: tar.TypeSymlink, Linkname: "../../escape"}}, []string{""})
	if e := u.tar(bytes.NewReader(b)); e == nil {
		t.Fatal("accepted escaping symlink")
	}
	u = unpacker{root: d}
	b = tarBytes(t, []*tar.Header{{Name: "lib/a", Typeflag: tar.TypeSymlink, Linkname: "b"}, {Name: "lib/b", Typeflag: tar.TypeSymlink, Linkname: "a"}}, []string{"", ""})
	if e := u.tar(bytes.NewReader(b)); e != nil {
		t.Fatal(e)
	}
	if e := u.resolve(); e == nil {
		t.Fatal("accepted cyclic links")
	}
}
func TestAPKMembersAndMaterializedLinks(t *testing.T) {
	var combined bytes.Buffer
	for _, b := range [][]byte{tarBytes(t, []*tar.Header{{Name: ".PKGINFO", Mode: 0644, Size: 7}}, []string{"control"}), tarBytes(t, []*tar.Header{{Name: "lib/real.so", Mode: 0755, Size: 7}, {Name: "lib/alias.so", Typeflag: tar.TypeSymlink, Linkname: "/lib/real.so"}}, []string{"library", ""})} {
		z := gzip.NewWriter(&combined)
		z.Write(b)
		z.Close()
	}
	d := t.TempDir()
	file := filepath.Join(d, "pkg.apk")
	os.WriteFile(file, combined.Bytes(), 0600)
	u := unpacker{root: filepath.Join(d, "payload"), filter: func(n string) bool { return strings.HasPrefix(n, "lib/") }}
	if e := u.gzip(file); e != nil {
		t.Fatal(e)
	}
	if e := u.resolve(); e != nil {
		t.Fatal(e)
	}
	b, e := os.ReadFile(filepath.Join(u.root, "lib", "alias.so"))
	if e != nil || string(b) != "library" {
		t.Fatalf("alias: %q %v", b, e)
	}
	st, _ := os.Lstat(filepath.Join(u.root, "lib", "alias.so"))
	if !st.Mode().IsRegular() {
		t.Fatal("link not materialized")
	}
	if _, e = os.Stat(filepath.Join(u.root, ".PKGINFO")); !os.IsNotExist(e) {
		t.Fatal("control file extracted")
	}
}
func TestBundleIntegrityAndTargetSelection(t *testing.T) {
	plans, e := Plans()
	if e != nil {
		t.Fatal(e)
	}
	for _, target := range []string{"windows/amd64", "windows/arm64", "darwin/amd64", "darwin/arm64", "linux/amd64", "linux/arm64"} {
		p, ok := plans[target]
		if !ok || len(p.Sources) < 2 {
			t.Fatal(target)
		}
		for _, s := range p.Sources {
			if len(s.SHA256) != 64 || !strings.HasPrefix(s.URL, "https://") {
				t.Fatal(s)
			}
		}
	}
	if plans["windows/arm64"].EmulatedArch != "amd64" {
		t.Fatal("Windows ARM emulation not declared")
	}
	d := t.TempDir()
	p := plans["windows/amd64"]
	for _, name := range []string{"openconnect.exe", "vpnc-script-win.js"} {
		os.WriteFile(filepath.Join(d, name), []byte("payload"), 0600)
	}
	if e = writeReceipt(d, p); e != nil {
		t.Fatal(e)
	}
	if e = verifyBundle(d, p); e != nil {
		t.Fatal(e)
	}
	os.WriteFile(filepath.Join(d, "openconnect.exe"), []byte("changed"), 0600)
	if e = verifyBundle(d, p); e == nil {
		t.Fatal("accepted corrupt core")
	}
}
func TestNativePortableCore(t *testing.T) {
	if os.Getenv("CUHK_NETWORK_TEST") != "1" {
		t.Skip("opt-in network and --version test; never connects VPN")
	}
	ctx, c := context.WithTimeout(context.Background(), 15*time.Minute)
	defer c()
	base := os.Getenv("CUHK_CORE_TEST_DIR")
	if base == "" {
		base = t.TempDir()
	}
	m := Manager{Base: base, OS: runtime.GOOS, Arch: runtime.GOARCH}
	d, e := m.Ensure(ctx, func(s string) { t.Log(s) })
	if e != nil {
		t.Fatal(e)
	}
	if e = checkExecutable(ctx, d, runtime.GOOS); e != nil {
		t.Fatal(e)
	}
	// Prove the installed bundle remains valid without network access.
	m.Client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, io.ErrClosedPipe })}
	if _, e = m.Ensure(ctx, func(string) {}); e != nil {
		t.Fatal(e)
	}
}

func TestFailedRepairPreservesPreviousFiles(t *testing.T) {
	p, e := planFor(runtime.GOOS, runtime.GOARCH)
	if e != nil {
		t.Skip(e)
	}
	base := t.TempDir()
	dir := bundleDir(base, runtime.GOOS, runtime.GOARCH, p)
	os.MkdirAll(dir, 0755)
	for _, n := range []string{coreName(runtime.GOOS), scriptName(runtime.GOOS)} {
		os.WriteFile(filepath.Join(dir, n), []byte("previous content"), 0600)
	}
	if e = writeReceipt(dir, p); e != nil {
		t.Fatal(e)
	}
	core := filepath.Join(dir, coreName(runtime.GOOS))
	os.WriteFile(core, []byte("needs repair"), 0600)
	m := Manager{Base: base, OS: runtime.GOOS, Arch: runtime.GOARCH, Client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, io.ErrClosedPipe })}}
	if _, e = m.Ensure(context.Background(), func(string) {}); e == nil {
		t.Fatal("repair unexpectedly succeeded")
	}
	b, e := os.ReadFile(core)
	if e != nil || string(b) != "needs repair" {
		t.Fatalf("previous files changed: %q %v", b, e)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
