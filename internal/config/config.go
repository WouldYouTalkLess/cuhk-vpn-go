package config

import (
	"cuhk-vpn-go/internal/appdir"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const (
	PasswordPortable = "portable"
	PasswordSystem   = "system"
)

type Settings struct {
	Server          string   `json:"server"`
	Group           string   `json:"group"`
	Username        string   `json:"username"`
	CIDRs           []string `json:"cidrs"`
	KeepDNS         bool     `json:"keepDNS"`
	CorePath        string   `json:"corePath"`
	Remember        bool     `json:"remember"`
	PasswordStorage string   `json:"passwordStorage"`
}

func Default() Settings {
	return Settings{Server: "https://vpn.cuhk.edu.cn", Group: "CUHK(SZ)", CIDRs: []string{"10.20.0.0/16"}, PasswordStorage: PasswordPortable}
}

func (s Settings) Validate(requireUser bool) (Settings, error) {
	// Profiles written by earlier versions used the system credential store.
	if s.PasswordStorage == "" {
		s.PasswordStorage = PasswordSystem
	}
	if s.PasswordStorage != PasswordPortable && s.PasswordStorage != PasswordSystem {
		return s, errors.New("无效的密码保存方式")
	}
	s.Server = strings.TrimSpace(s.Server)
	u, err := url.Parse(s.Server)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || u.RawQuery != "" {
		return s, errors.New("VPN 地址必须是 HTTPS 地址，且不能包含账号、查询参数或片段")
	}
	s.Username = strings.TrimSpace(s.Username)
	if requireUser && s.Username == "" {
		return s, errors.New("请输入学校账号")
	}
	for _, v := range []string{s.Server, s.Group, s.Username, s.CorePath} {
		if strings.ContainsAny(v, "\x00\r\n") {
			return s, errors.New("设置中不能含换行或空字符")
		}
	}
	if s.CorePath != "" && !filepath.IsAbs(s.CorePath) {
		return s, errors.New("核心路径必须是绝对路径")
	}
	if len(s.CIDRs) == 0 || len(s.CIDRs) > 64 {
		return s, errors.New("请填写 1 到 64 个 IPv4 网段")
	}
	seen := map[string]bool{}
	routes := []string{}
	for _, raw := range s.CIDRs {
		p, e := netip.ParsePrefix(strings.TrimSpace(raw))
		if e != nil || !p.Addr().Is4() || p.Bits() == 0 {
			return s, fmt.Errorf("无效的 IPv4 分流网段 %q（不允许默认路由）", raw)
		}
		p = p.Masked()
		if !seen[p.String()] {
			seen[p.String()] = true
			routes = append(routes, p.String())
		}
	}
	// Reject combinations such as the two /1 routes that would recreate a full tunnel.
	if coversAll(routes, netip.MustParsePrefix("0.0.0.0/0")) {
		return s, errors.New("分流网段合起来覆盖了整个 IPv4 网络")
	}
	s.CIDRs = routes
	return s, nil
}

func coversAll(routes []string, parent netip.Prefix) bool {
	var children []string
	for _, r := range routes {
		p, _ := netip.ParsePrefix(r)
		if p.Bits() <= parent.Bits() && p.Contains(parent.Addr()) {
			return true
		}
		if parent.Contains(p.Addr()) {
			children = append(children, r)
		}
	}
	if len(children) < 2 || parent.Bits() == 32 {
		return false
	}
	b := parent.Addr().As4()
	n := uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
	n |= uint32(1) << (31 - parent.Bits())
	right := netip.AddrFrom4([4]byte{byte(n >> 24), byte(n >> 16), byte(n >> 8), byte(n)})
	return coversAll(children, netip.PrefixFrom(parent.Addr(), parent.Bits()+1)) && coversAll(children, netip.PrefixFrom(right, parent.Bits()+1))
}

func Directory() (string, error) {
	p, e := appdir.Directory()
	return filepath.Join(p, "data"), e
}
func Load() (Settings, error) {
	d, e := Directory()
	if e != nil {
		return Default(), e
	}
	p, e := os.UserConfigDir()
	if e != nil {
		return Default(), e
	}
	return load(d, filepath.Join(p, "CuhkVpnGo"))
}

func load(directory, legacyDirectory string) (Settings, error) {
	s := Default()
	d := directory
	b, e := os.ReadFile(filepath.Join(d, "settings.json"))
	if os.IsNotExist(e) && legacyDirectory != "" {
		b, e = os.ReadFile(filepath.Join(legacyDirectory, "settings.json"))
	}
	if os.IsNotExist(e) {
		return s, nil
	}
	if e != nil {
		return s, e
	}
	// A missing storage field means this is an older system-keyring profile.
	s.PasswordStorage = PasswordSystem
	if e = json.Unmarshal(b, &s); e != nil {
		return Default(), e
	}
	return s.Validate(false)
}
func Save(s Settings) error {
	d, e := Directory()
	if e != nil {
		return e
	}
	b, e := json.MarshalIndent(s, "", "  ")
	if e != nil {
		return e
	}
	return WritePrivateFile(filepath.Join(d, "settings.json"), b)
}

// WritePrivateFile publishes a complete file atomically, keeping the old file
// intact on failure. Callers must not put a plaintext password in settings.
func WritePrivateFile(path string, b []byte) error {
	d := filepath.Dir(path)
	if e := os.MkdirAll(d, 0700); e != nil {
		return e
	}
	f, e := os.CreateTemp(d, ".write-*.tmp")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if _, e = f.Write(b); e != nil {
		f.Close()
		return e
	}
	if e = f.Sync(); e != nil {
		f.Close()
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	return replaceFile(f.Name(), path)
}
