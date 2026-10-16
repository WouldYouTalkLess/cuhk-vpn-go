package core

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"errors"
	"fmt"
	"github.com/bodgit/sevenzip"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

func safePath(root, name string) (string, error) {
	// Check both separator styles, even when inspecting a foreign platform bundle.
	name = strings.ReplaceAll(name, "\\", "/")
	if strings.ContainsRune(name, 0) || strings.Contains(name, ":") || strings.HasPrefix(name, "/") {
		return "", fmt.Errorf("压缩包路径非法：%q", name)
	}
	clean := path.Clean(name)
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("压缩包路径越界：%q", name)
	}
	p := filepath.Join(root, filepath.FromSlash(clean))
	rel, e := filepath.Rel(root, p)
	if e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("压缩包路径越界")
	}
	return p, nil
}

type unpacker struct {
	root   string
	links  map[string]string
	total  int64
	filter func(string) bool
}

func (u *unpacker) put(name string, r io.Reader, size int64, mode os.FileMode) error {
	p, e := safePath(u.root, name)
	if e != nil {
		return e
	}
	if u.filter != nil && !u.filter(filepath.ToSlash(name)) {
		return nil
	}
	if size < 0 || size > 128<<20 || u.total+size > 768<<20 {
		return errors.New("压缩包展开大小超过上限")
	}
	u.total += size
	if e = os.MkdirAll(filepath.Dir(p), 0755); e != nil {
		return e
	}
	permissions := os.FileMode(0644)
	if mode&0111 != 0 {
		permissions = 0755
	}
	if st, e := os.Lstat(p); e == nil && !st.Mode().IsRegular() {
		return errors.New("压缩包试图覆盖非普通文件")
	}
	f, e := os.OpenFile(p, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, permissions)
	if e != nil {
		return e
	}
	n, e := io.Copy(f, io.LimitReader(r, size+1))
	ce := f.Close()
	if e != nil {
		return e
	}
	if ce != nil {
		return ce
	}
	if n != size {
		return io.ErrUnexpectedEOF
	}
	return os.Chmod(p, permissions)
}
func (u *unpacker) tar(r io.Reader) error {
	t := tar.NewReader(r)
	for {
		h, e := t.Next()
		if e == io.EOF {
			return nil
		}
		if e != nil {
			return e
		}
		if _, e = safePath(u.root, h.Name); e != nil {
			return e
		}
		if u.filter != nil && !u.filter(h.Name) {
			continue
		}
		switch h.Typeflag {
		case tar.TypeDir:
			continue
		case tar.TypeReg, tar.TypeRegA:
			if e = u.put(h.Name, t, h.Size, os.FileMode(h.Mode)); e != nil {
				return e
			}
		case tar.TypeSymlink, tar.TypeLink:
			name := path.Clean(strings.ReplaceAll(h.Name, "\\", "/"))
			target := strings.ReplaceAll(h.Linkname, "\\", "/")
			if strings.HasPrefix(target, "/") {
				target = strings.TrimLeft(target, "/")
			} else if h.Typeflag == tar.TypeSymlink {
				target = path.Join(path.Dir(name), target)
			}
			if _, e = safePath(u.root, target); e != nil {
				return e
			}
			if u.links == nil {
				u.links = map[string]string{}
			}
			u.links[name] = target
		default:
			return fmt.Errorf("压缩包包含不支持的特殊文件 %s", h.Name)
		}
	}
}
func (u *unpacker) gzip(file string) error {
	f, e := os.Open(file)
	if e != nil {
		return e
	}
	defer f.Close()
	b := bufio.NewReader(f)
	// APK v2 consists of separate gzip members (signature, control, payload).
	// Parse each tar independently instead of stopping at the first tar EOF.
	for {
		if _, e = b.Peek(1); e == io.EOF {
			return nil
		} else if e != nil {
			return e
		}
		z, e := gzip.NewReader(b)
		if e != nil {
			return e
		}
		z.Multistream(false)
		e = u.tar(z)
		if e == nil {
			_, e = io.Copy(io.Discard, io.LimitReader(z, (768<<20)+1))
		}
		ce := z.Close()
		if e != nil {
			return e
		}
		if ce != nil {
			return ce
		}
	}
}
func (u *unpacker) resolve() error {
	var resolve func(string, map[string]bool) (string, error)
	resolve = func(name string, visiting map[string]bool) (string, error) {
		if visiting[name] {
			return "", errors.New("压缩包符号链接形成循环")
		}
		target, ok := u.links[name]
		if !ok {
			return safePath(u.root, name)
		}
		visiting[name] = true
		p, e := resolve(target, visiting)
		delete(visiting, name)
		return p, e
	}
	for name := range u.links {
		src, e := resolve(name, map[string]bool{})
		if e != nil {
			return e
		}
		f, e := os.Open(src)
		if e != nil {
			return fmt.Errorf("无法解析便携库链接 %s: %w", name, e)
		}
		st, e := f.Stat()
		if e == nil {
			e = u.put(name, f, st.Size(), st.Mode())
		}
		f.Close()
		if e != nil {
			return e
		}
	}
	return nil
}
func extract7zip(file, dest string) error {
	f, e := os.Open(file)
	if e != nil {
		return e
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil {
		return e
	}
	z, e := sevenzip.NewReader(f, st.Size())
	if e != nil {
		return e
	}
	u := unpacker{root: dest}
	for _, entry := range z.File {
		base := path.Base(strings.ReplaceAll(entry.Name, "\\", "/"))
		if base != "7z.exe" && base != "7z.dll" && base != "License.txt" {
			continue
		}
		r, e := entry.Open()
		if e != nil {
			return e
		}
		e = u.put(base, r, int64(entry.UncompressedSize), 0755)
		r.Close()
		if e != nil {
			return e
		}
	}
	for _, name := range []string{"7z.exe", "7z.dll"} {
		if _, e = os.Stat(filepath.Join(dest, name)); e != nil {
			return fmt.Errorf("便携解压工具缺少 %s", name)
		}
	}
	return nil
}
