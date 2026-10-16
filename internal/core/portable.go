package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

type Manager struct {
	Base, OS, Arch string
	Client         *http.Client
	AllowHTTP      bool
}

var preparation sync.Mutex

type receipt struct {
	Plan  string            `json:"plan"`
	Files map[string]string `json:"files"`
}

func (m Manager) Ensure(ctx context.Context, log func(string)) (string, error) {
	preparation.Lock()
	defer preparation.Unlock()
	p, e := planFor(m.OS, m.Arch)
	if e != nil {
		return "", e
	}
	if e = checkHost(ctx, m.OS, p); e != nil {
		return "", e
	}
	final := bundleDir(m.Base, m.OS, m.Arch, p)
	if e = verifyBundle(final, p); e == nil {
		return final, nil
	}
	if e = os.MkdirAll(filepath.Dir(final), 0755); e != nil {
		return "", fmt.Errorf("应用目录不可写，请将整个应用放到可写目录：%w", e)
	}
	stage, e := os.MkdirTemp(filepath.Dir(final), ".prepare-")
	if e != nil {
		return "", e
	}
	defer os.RemoveAll(stage)
	log(fmt.Sprintf("准备 %s/%s 便携核心 %s…", m.OS, m.Arch, p.Version))
	if p.EmulatedArch != "" {
		log("此平台使用 " + p.EmulatedArch + " 核心，需要系统提供相应的架构仿真")
	}
	paths := map[string]string{}
	for n, s := range p.Sources {
		if e = ctx.Err(); e != nil {
			return "", e
		}
		log(fmt.Sprintf("下载并校验依赖 %d/%d · %s", n+1, len(p.Sources), s.Name))
		file, err := m.download(ctx, s)
		if err != nil {
			return "", err
		}
		paths[s.Name] = file
	}
	log("所有压缩包 SHA-256 校验通过，正在解压运行库…")
	if e = assemble(ctx, stage, m.OS, m.Arch, p, paths); e != nil {
		return "", e
	}
	if b, e := json.MarshalIndent(p, "", "  "); e != nil {
		return "", e
	} else if e = os.WriteFile(filepath.Join(stage, "origins.json"), b, 0644); e != nil {
		return "", e
	}
	if e = writeReceipt(stage, p); e != nil {
		return "", e
	}
	if e = verifyBundle(stage, p); e != nil {
		return "", e
	}
	if m.OS == runtime.GOOS && m.Arch == runtime.GOARCH {
		if e = checkExecutable(ctx, stage, m.OS); e != nil {
			return "", e
		}
	}
	// Publish only a complete, validated bundle. Preserve any previous directory
	// until the new one is ready and restore it if the final rename fails.
	backup := ""
	if _, e = os.Lstat(final); e == nil {
		backup = stage + "-previous"
		if e = os.Rename(final, backup); e != nil {
			return "", fmt.Errorf("旧核心仍在使用，无法替换：%w", e)
		}
	}
	if e = os.Rename(stage, final); e != nil {
		if backup != "" {
			os.Rename(backup, final)
		}
		return "", e
	}
	if backup != "" {
		os.RemoveAll(backup)
	}
	log("便携核心已就绪 · " + final)
	return final, nil
}
func (m Manager) download(ctx context.Context, s Source) (string, error) {
	if len(s.SHA256) != 64 {
		return "", errors.New("核心清单缺少 SHA-256")
	}
	if _, e := hex.DecodeString(s.SHA256); e != nil {
		return "", e
	}
	u, e := url.Parse(s.URL)
	if e != nil || u.User != nil || (u.Scheme != "https" && !(m.AllowHTTP && u.Scheme == "http")) {
		return "", errors.New("下载地址必须使用 HTTPS")
	}
	d := filepath.Join(m.Base, "cores", ".cache")
	if e = os.MkdirAll(d, 0700); e != nil {
		return "", e
	}
	target := filepath.Join(d, s.SHA256)
	if hash, e := fileHash(target); e == nil && hash == s.SHA256 {
		return target, nil
	}
	client := m.Client
	if client == nil {
		client = &http.Client{Timeout: 8 * time.Minute, CheckRedirect: func(r *http.Request, via []*http.Request) error {
			if r.URL.Scheme != "https" || len(via) > 8 {
				return errors.New("拒绝不安全的下载重定向")
			}
			return nil
		}}
	}
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, s.URL, nil)
	if e != nil {
		return "", e
	}
	req.Header.Set("User-Agent", "CuhkVpnGo/1.1")
	if u.Host == "ghcr.io" {
		repo := strings.TrimPrefix(strings.Split(u.Path, "/blobs/")[0], "/v2/")
		tokenURL := "https://ghcr.io/token?service=ghcr.io&scope=" + url.QueryEscape("repository:"+repo+":pull")
		tr, e := http.NewRequestWithContext(ctx, http.MethodGet, tokenURL, nil)
		if e != nil {
			return "", e
		}
		resp, e := client.Do(tr)
		if e != nil {
			return "", e
		}
		var tok struct {
			Token string `json:"token"`
		}
		e = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&tok)
		resp.Body.Close()
		if e != nil || tok.Token == "" {
			return "", errors.New("无法获取公开 bottle 下载授权")
		}
		req.Header.Set("Authorization", "Bearer "+tok.Token)
	}
	resp, e := client.Do(req)
	if e != nil {
		return "", e
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("下载 %s 返回 HTTP %d", s.Name, resp.StatusCode)
	}
	f, e := os.CreateTemp(d, ".download-")
	if e != nil {
		return "", e
	}
	defer os.Remove(f.Name())
	h := sha256.New()
	n, e := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, (128<<20)+1))
	ce := f.Close()
	if e != nil {
		return "", e
	}
	if ce != nil {
		return "", ce
	}
	if n > 128<<20 || hex.EncodeToString(h.Sum(nil)) != s.SHA256 {
		return "", fmt.Errorf("%s 的 SHA-256 校验失败，已拒绝解压", s.Name)
	}
	// A corrupt cache entry is ours (its name is a verified hex digest).
	if _, e = os.Lstat(target); e == nil {
		if e = os.Remove(target); e != nil {
			return "", e
		}
	}
	if e = os.Rename(f.Name(), target); e != nil {
		return "", e
	}
	return target, nil
}
func fileHash(p string) (string, error) {
	f, e := os.Open(p)
	if e != nil {
		return "", e
	}
	defer f.Close()
	h := sha256.New()
	if _, e = io.Copy(h, f); e != nil {
		return "", e
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func writeReceipt(dir string, p Plan) error {
	r := receipt{Plan: planID(p), Files: map[string]string{}}
	e := filepath.WalkDir(dir, func(path string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return errors.New("核心包不能含未经处理的符号链接")
		}
		rel, _ := filepath.Rel(dir, path)
		if rel == "bundle.json" {
			return nil
		}
		h, e := fileHash(path)
		if e == nil {
			r.Files[filepath.ToSlash(rel)] = h
		}
		return e
	})
	if e != nil {
		return e
	}
	b, e := json.MarshalIndent(r, "", "  ")
	if e != nil {
		return e
	}
	return os.WriteFile(filepath.Join(dir, "bundle.json"), b, 0600)
}
func verifyBundle(dir string, p Plan) error {
	b, e := os.ReadFile(filepath.Join(dir, "bundle.json"))
	if e != nil {
		return e
	}
	var r receipt
	if e = json.Unmarshal(b, &r); e != nil {
		return e
	}
	if r.Plan != planID(p) || len(r.Files) == 0 {
		return errors.New("核心包版本不匹配")
	}
	names := make([]string, 0, len(r.Files))
	for n := range r.Files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		path, e := safePath(dir, n)
		if e != nil {
			return e
		}
		st, e := os.Lstat(path)
		if e != nil {
			return e
		}
		if !st.Mode().IsRegular() {
			return fmt.Errorf("核心文件不是普通文件：%s", n)
		}
		h, e := fileHash(path)
		if e != nil {
			return e
		}
		if h != r.Files[n] {
			return fmt.Errorf("核心文件损坏：%s", n)
		}
	}
	return nil
}
