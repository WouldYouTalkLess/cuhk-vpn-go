package core

import (
	"context"
	"cuhk-vpn-go/internal/platform"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

func assemble(ctx context.Context, dir, goos, arch string, p Plan, paths map[string]string) error {
	if goos == "windows" {
		return assembleWindows(ctx, dir, p, paths)
	}
	payload := filepath.Join(dir, ".payload")
	u := unpacker{root: payload}
	if goos == "linux" {
		u.filter = func(name string) bool {
			for _, prefix := range []string{"lib/", "usr/lib/", "usr/bin/", "usr/sbin/", "sbin/", "bin/", "etc/ssl/", "usr/share/licenses/"} {
				if strings.HasPrefix(name, prefix) {
					return true
				}
			}
			return false
		}
	}
	for _, s := range p.Sources {
		switch s.Format {
		case "bottle", "apk":
			if e := u.gzip(paths[s.Name]); e != nil {
				return fmt.Errorf("解压 %s: %w", s.Name, e)
			}
		case "file":
			if e := copyFile(paths[s.Name], filepath.Join(dir, "vpnc-script"), 0755); e != nil {
				return e
			}
		default:
			return fmt.Errorf("未知的便携包格式 %s", s.Format)
		}
	}
	if e := u.resolve(); e != nil {
		return e
	}
	if goos == "darwin" {
		if e := assembleMac(payload, dir); e != nil {
			return e
		}
		return os.RemoveAll(payload)
	}
	if goos != "linux" {
		return fmt.Errorf("不支持 %s", goos)
	}
	// Alpine's musl loader is bundled and invoked explicitly, so the target
	// distribution does not need to provide musl or any of OpenConnect's libraries.
	for _, name := range []string{"lib", "usr", "sbin", "etc"} {
		src := filepath.Join(payload, name)
		if _, e := os.Stat(src); e == nil {
			if e = os.Rename(src, filepath.Join(dir, name)); e != nil {
				return e
			}
		}
	}
	// Some APKs keep required libraries in private subdirectories (libproxy,
	// xmlsec, ...). Put the ELF library closure on our explicit loader path.
	if e := flattenLinuxLibraries(dir); e != nil {
		return e
	}
	loader := "lib/ld-musl-x86_64.so.1"
	if arch == "arm64" {
		loader = "lib/ld-musl-aarch64.so.1"
	}
	if _, e := os.Stat(filepath.Join(dir, filepath.FromSlash(loader))); e != nil {
		return e
	}
	core, e := findFile(dir, "openconnect")
	if e != nil {
		return e
	}
	if e = validateELF(dir, core, arch); e != nil {
		return e
	}
	rel, _ := filepath.Rel(dir, core)
	cert, e := findFile(filepath.Join(dir, "etc"), "ca-certificates.crt")
	if e != nil {
		return e
	}
	if e = copyFile(cert, filepath.Join(dir, "cacert.pem"), 0644); e != nil {
		return e
	}
	if e = writeLauncher(filepath.Join(dir, "openconnect"), loader, filepath.ToSlash(rel), true, false); e != nil {
		return e
	}
	ip, e := findFile(dir, "ip")
	if e != nil {
		return e
	}
	if e = validateELF(dir, ip, arch); e != nil {
		return e
	}
	ipRel, _ := filepath.Rel(dir, ip)
	if e = writeLauncher(filepath.Join(dir, "tools", "ip"), loader, filepath.ToSlash(ipRel), false, true); e != nil {
		return e
	}
	return os.RemoveAll(payload)
}
func assembleWindows(ctx context.Context, dir string, p Plan, paths map[string]string) error {
	tool := filepath.Join(dir, ".extractor")
	var nsis string
	for _, s := range p.Sources {
		if s.Format == "7zip-sfx" {
			if e := extract7zip(paths[s.Name], tool); e != nil {
				return e
			}
		} else if s.Format == "nsis" {
			nsis = paths[s.Name]
		}
	}
	if runtime.GOOS != "windows" {
		return fmt.Errorf("Windows NSIS 便携包需要在 Windows 上解压")
	}
	cmd := exec.CommandContext(ctx, filepath.Join(tool, "7z.exe"), "x", nsis, "-o"+dir, "-y", "openconnect.exe", "*.dll", "vpnc-script-win.js", "-xr!$PLUGINSDIR")
	platform.StartHidden(cmd)
	if b, e := cmd.CombinedOutput(); e != nil {
		return platform.CommandError("解压 Windows 核心", e, b)
	}
	for _, name := range []string{"openconnect.exe", "wintun.dll", "libopenconnect-5.dll", "vpnc-script-win.js"} {
		if _, e := os.Stat(filepath.Join(dir, name)); e != nil {
			return e
		}
	}
	return os.RemoveAll(tool)
}
func copyFile(src, dst string, mode os.FileMode) error {
	if e := os.MkdirAll(filepath.Dir(dst), 0755); e != nil {
		return e
	}
	f, e := os.Open(src)
	if e != nil {
		return e
	}
	defer f.Close()
	w, e := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if e != nil {
		return e
	}
	_, e = io.Copy(w, f)
	ce := w.Close()
	if e != nil {
		return e
	}
	if ce != nil {
		return ce
	}
	return os.Chmod(dst, mode)
}
func findFile(dir, base string) (string, error) {
	var found string
	e := filepath.WalkDir(dir, func(p string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if !d.IsDir() && d.Name() == base && found == "" {
			found = p
		}
		return nil
	})
	if e != nil {
		return "", e
	}
	if found == "" {
		return "", fmt.Errorf("便携包缺少 %s", base)
	}
	return found, nil
}
func writeLauncher(dst, loader, binary string, ca, tool bool) error {
	if e := os.MkdirAll(filepath.Dir(dst), 0755); e != nil {
		return e
	}
	prefix := ""
	if tool {
		prefix = "/.."
	}
	body := "#!/bin/sh\nset -eu\nbase=$(CDPATH= cd -- \"$(dirname -- \"$0\")" + prefix + "\" && pwd)\nunset LD_PRELOAD LD_LIBRARY_PATH DYLD_INSERT_LIBRARIES DYLD_LIBRARY_PATH\n"
	if loader != "" {
		body += "exec \"$base/" + loader + "\" --library-path \"$base/lib:$base/usr/lib\" \"$base/" + binary + "\""
	} else {
		body += "exec \"$base/" + binary + "\""
	}
	if ca {
		body += " --cafile \"$base/cacert.pem\""
	}
	body += " \"$@\"\n"
	return os.WriteFile(dst, []byte(body), 0755)
}
func checkExecutable(ctx context.Context, dir, goos string) error {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, filepath.Join(dir, coreName(goos)), "--version")
	platform.StartHidden(cmd)
	b, e := cmd.CombinedOutput()
	if e != nil {
		return platform.CommandError("便携核心自检", e, b)
	}
	if !strings.Contains(string(b), "OpenConnect version") {
		return fmt.Errorf("下载包中的核心未返回 OpenConnect 版本")
	}
	return nil
}
