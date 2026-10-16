package core

import (
	"bytes"
	"debug/elf"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func flattenLinuxLibraries(root string) error {
	var files []string
	for _, sub := range []string{"lib", "usr/lib"} {
		dir := filepath.Join(root, filepath.FromSlash(sub))
		if _, e := os.Stat(dir); os.IsNotExist(e) {
			continue
		}
		if e := filepath.WalkDir(dir, func(p string, d os.DirEntry, e error) error {
			if e != nil {
				return e
			}
			if !d.IsDir() && strings.Contains(d.Name(), ".so") {
				files = append(files, p)
			}
			return nil
		}); e != nil {
			return e
		}
	}
	for _, src := range files {
		dest := filepath.Join(root, "lib", filepath.Base(src))
		if filepath.Clean(src) == filepath.Clean(dest) {
			continue
		}
		f, e := os.Open(src)
		if e != nil {
			return e
		}
		magic := make([]byte, 4)
		_, e = f.Read(magic)
		f.Close()
		if e != nil || !bytes.Equal(magic, []byte{0x7f, 'E', 'L', 'F'}) {
			continue
		}
		if _, e = os.Stat(dest); e == nil {
			a, _ := fileHash(src)
			b, _ := fileHash(dest)
			if a != b {
				return fmt.Errorf("Linux 动态库名称冲突：%s", filepath.Base(src))
			}
			continue
		}
		if e = copyFile(src, dest, 0755); e != nil {
			return e
		}
	}
	return nil
}

func validateELF(root, main, arch string) error {
	want := elf.EM_X86_64
	if arch == "arm64" {
		want = elf.EM_AARCH64
	}
	seen := map[string]bool{}
	var visit func(string) error
	visit = func(p string) error {
		if seen[p] {
			return nil
		}
		seen[p] = true
		f, e := elf.Open(p)
		if e != nil {
			return e
		}
		defer f.Close()
		if f.Machine != want {
			return fmt.Errorf("ELF 架构不匹配：%s", p)
		}
		deps, e := f.ImportedLibraries()
		if e != nil {
			return e
		}
		for _, name := range deps {
			found := ""
			for _, d := range []string{"lib", "usr/lib"} {
				candidate := filepath.Join(root, filepath.FromSlash(d), name)
				if _, e := os.Stat(candidate); e == nil {
					found = candidate
					break
				}
			}
			if found == "" {
				return fmt.Errorf("便携核心缺少 ELF 运行库 %s", name)
			}
			if e = visit(found); e != nil {
				return e
			}
		}
		return nil
	}
	return visit(main)
}
