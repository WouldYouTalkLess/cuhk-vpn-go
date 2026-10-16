package core

import (
	"context"
	"crypto/sha256"
	"cuhk-vpn-go/internal/appdir"
	"cuhk-vpn-go/internal/platform"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

//go:embed sources.json
var locks embed.FS

type Source struct {
	Name   string `json:"name"`
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Format string `json:"format"`
}
type Plan struct {
	Version      string   `json:"version"`
	MinOS        string   `json:"minOS,omitempty"`
	EmulatedArch string   `json:"emulatedArch,omitempty"`
	Sources      []Source `json:"sources"`
}
type Info struct {
	OS        string `json:"os"`
	Arch      string `json:"arch"`
	Path      string `json:"path"`
	Script    string `json:"script"`
	Version   string `json:"version"`
	Ready     bool   `json:"ready"`
	Error     string `json:"error"`
	Source    string `json:"source"`
	Directory string `json:"directory"`
}

func Plans() (map[string]Plan, error) {
	var data struct {
		Schema int             `json:"schema"`
		Plans  map[string]Plan `json:"plans"`
	}
	b, e := locks.ReadFile("sources.json")
	if e == nil {
		e = json.Unmarshal(b, &data)
	}
	if e != nil {
		return nil, e
	}
	if data.Schema != 1 {
		return nil, fmt.Errorf("不支持的核心清单版本")
	}
	return data.Plans, nil
}
func planFor(goos, arch string) (Plan, error) {
	p, e := Plans()
	if e != nil {
		return Plan{}, e
	}
	v, ok := p[goos+"/"+arch]
	if !ok {
		return v, fmt.Errorf("没有 %s/%s 的便携核心", goos, arch)
	}
	return v, nil
}
func planID(p Plan) string {
	b, _ := json.Marshal(p)
	b = append(b, []byte("portable-layout-v1")...)
	if p.MinOS != "" {
		b = append(b, []byte("macos-go-adhoc-v1")...)
	}
	h := sha256.Sum256(b)
	return p.Version + "-" + hex.EncodeToString(h[:6])
}
func ApplicationDir() (string, error) {
	return appdir.Directory()
}
func bundleDir(base, goos, arch string, p Plan) string {
	return filepath.Join(base, "cores", goos+"-"+arch, planID(p))
}
func coreName(goos string) string {
	if goos == "windows" {
		return "openconnect.exe"
	}
	return "openconnect"
}
func scriptName(goos string) string {
	if goos == "windows" {
		return "vpnc-script-win.js"
	}
	return "vpnc-script"
}
func Find(custom string) Info {
	base, e := ApplicationDir()
	if e != nil {
		return Info{OS: runtime.GOOS, Arch: runtime.GOARCH, Error: e.Error()}
	}
	p, e := planFor(runtime.GOOS, runtime.GOARCH)
	i := Info{OS: runtime.GOOS, Arch: runtime.GOARCH, Source: "应用内便携核心 · 自动下载 / SHA-256 / 解压"}
	if e != nil {
		i.Error = e.Error()
		return i
	}
	i.Directory = bundleDir(base, runtime.GOOS, runtime.GOARCH, p)
	if custom == "" {
		i.Path = filepath.Join(i.Directory, coreName(runtime.GOOS))
		if e = verifyBundle(i.Directory, p); e != nil {
			i.Error = "便携核心尚未就绪：" + e.Error()
			return i
		}
	} else {
		i.Path = custom
		i.Directory = filepath.Dir(custom)
		i.Source = "自定义核心"
	}
	i.Script = filepath.Join(i.Directory, scriptName(runtime.GOOS))
	if _, e = os.Stat(i.Script); e != nil {
		if runtime.GOOS != "windows" {
			i.Script = filepath.Join(i.Directory, "..", "vpnc-script")
		}
		if _, e = os.Stat(i.Script); e != nil {
			i.Error = "核心目录缺少配套 vpnc-script"
			return i
		}
	}
	ctx, c := context.WithTimeout(context.Background(), 8*time.Second)
	defer c()
	cmd := exec.CommandContext(ctx, i.Path, "--version")
	platform.StartHidden(cmd)
	b, e := cmd.CombinedOutput()
	if e != nil {
		i.Error = fmt.Sprintf("便携核心无法运行：%v · %s", e, strings.TrimSpace(string(b)))
		return i
	}
	i.Version = strings.TrimSpace(strings.SplitN(strings.TrimSpace(string(b)), "\n", 2)[0])
	i.Ready = true
	return i
}
func Prepare(ctx context.Context, log func(string)) (Info, error) {
	base, e := ApplicationDir()
	if e != nil {
		return Info{}, e
	}
	m := Manager{Base: base, OS: runtime.GOOS, Arch: runtime.GOARCH}
	dir, e := m.Ensure(ctx, log)
	if e != nil {
		return Info{}, e
	}
	i := Find("")
	if !i.Ready {
		return i, fmt.Errorf("%s (%s)", i.Error, dir)
	}
	return i, nil
}
func ToolsDir(i Info) string { return filepath.Join(i.Directory, "tools") }
