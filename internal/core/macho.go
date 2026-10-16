package core

import (
	"bytes"
	"context"
	"debug/macho"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

func checkHost(ctx context.Context, goos string, p Plan) error {
	if goos != "darwin" || runtime.GOOS != "darwin" || p.MinOS == "" {
		return nil
	}
	b, e := exec.CommandContext(ctx, "/usr/bin/sw_vers", "-productVersion").Output()
	if e != nil {
		return e
	}
	major, e := strconv.Atoi(strings.Split(strings.TrimSpace(string(b)), ".")[0])
	if e != nil {
		return e
	}
	minimum, _ := strconv.Atoi(strings.Split(p.MinOS, ".")[0])
	if major < minimum {
		return fmt.Errorf("此便携核心要求 macOS %s 或更新版本", p.MinOS)
	}
	return nil
}

func assembleMac(payload, dir string) error {
	bin, e := findFile(payload, "openconnect")
	if e != nil {
		return e
	}
	if e = copyFile(bin, filepath.Join(dir, "bin", "openconnect"), 0755); e != nil {
		return e
	}
	cert, e := findFile(payload, "cacert.pem")
	if e != nil {
		return e
	}
	if e = copyFile(cert, filepath.Join(dir, "cacert.pem"), 0644); e != nil {
		return e
	}
	files := []string{filepath.Join(dir, "bin", "openconnect")}
	libraryNames := map[string]bool{}
	e = filepath.WalkDir(payload, func(p string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".dylib") {
			return nil
		}
		dest := filepath.Join(dir, "lib", d.Name())
		if libraryNames[d.Name()] {
			a, _ := fileHash(dest)
			b, _ := fileHash(p)
			if a != b {
				return fmt.Errorf("动态库名称冲突：%s", d.Name())
			}
			return nil
		}
		libraryNames[d.Name()] = true
		if e = copyFile(p, dest, 0755); e == nil {
			files = append(files, dest)
		}
		return e
	})
	if e != nil {
		return e
	}
	for _, p := range files {
		if e = relocateMachO(p, libraryNames, filepath.Base(filepath.Dir(p)) == "bin"); e != nil {
			return e
		}
	}
	return writeLauncher(filepath.Join(dir, "openconnect"), "", "bin/openconnect", true, false)
}
func relocateMachO(file string, libraries map[string]bool, executable bool) error {
	b, e := os.ReadFile(file)
	if e != nil {
		return e
	}
	m, e := macho.NewFile(bytes.NewReader(b))
	if e != nil {
		return e
	}
	defer m.Close()
	header := 32
	if m.Magic == macho.Magic32 {
		header = 28
	}
	o := header
	order := m.ByteOrder
	for i := uint32(0); i < m.Ncmd; i++ {
		if o+8 > len(b) {
			return fmt.Errorf("Mach-O load command 越界")
		}
		cmd := order.Uint32(b[o : o+4])
		size := int(order.Uint32(b[o+4 : o+8]))
		if size < 8 || o+size > len(b) {
			return fmt.Errorf("无效的 Mach-O load command")
		}
		if cmd == 0xc || cmd == 0xd || cmd == 0x80000018 || cmd == 0x8000001f || cmd == 0x80000023 {
			if size < 24 {
				return fmt.Errorf("无效的动态库命令")
			}
			start := int(order.Uint32(b[o+8 : o+12]))
			if start < 24 || start >= size {
				return fmt.Errorf("无效的动态库名称")
			}
			slot := b[o+start : o+size]
			n := bytes.IndexByte(slot, 0)
			if n < 0 {
				return fmt.Errorf("动态库名称未终止")
			}
			old := string(slot[:n])
			base := filepath.Base(old)
			if libraries[base] {
				prefix := "@loader_path/"
				if executable {
					prefix = "@loader_path/../lib/"
				}
				replacement := prefix + base
				if len(replacement)+1 > len(slot) {
					return fmt.Errorf("无法重定位动态库 %s", old)
				}
				clear(slot)
				copy(slot, replacement)
			} else if !strings.HasPrefix(old, "/usr/lib/") && !strings.HasPrefix(old, "/System/Library/") {
				return fmt.Errorf("便携包缺少动态库：%s", old)
			}
		}
		o += size
	}
	if e = signMachO(b); e != nil {
		return fmt.Errorf("重建便携库签名 %s: %w", file, e)
	}
	return os.WriteFile(file, b, 0755)
}
