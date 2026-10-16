package vpn

import (
	"cuhk-vpn-go/internal/config"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestWindowsWrapperEnvironment(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows script host")
	}
	d := t.TempDir()
	official := filepath.Join(d, "official ' space.js")
	marker := filepath.Join(d, "state")
	capture := filepath.Join(d, "captured.txt")
	stub := `var e=WScript.CreateObject('WScript.Shell').Environment('Process');var f=WScript.CreateObject('Scripting.FileSystemObject').CreateTextFile(` + jsQuote(capture) + `,true);f.Write(e('CISCO_SPLIT_INC')+'|'+e('CISCO_SPLIT_INC_0_ADDR')+'|'+e('CISCO_SPLIT_INC_0_MASK')+'|'+e('CISCO_SPLIT_EXC')+'|'+e('CISCO_IPV6_SPLIT_INC')+'|'+e('INTERNAL_IP4_DNS'));f.Close();WScript.Quit(0);`
	if e := os.WriteFile(official, []byte(stub), 0600); e != nil {
		t.Fatal(e)
	}
	s := config.Default()
	script := filepath.Join(d, "wrapper.js")
	os.WriteFile(script, []byte(WindowsScript(official, marker, s)), 0600)
	cmd := exec.Command("cscript.exe", "//NoLogo", script)
	cmd.Env = append(os.Environ(), "reason=connect", "INTERNAL_IP4_ADDRESS=10.20.1.2", "TUNDEV=test", "CISCO_SPLIT_INC=99", "CISCO_SPLIT_EXC=5", "INTERNAL_IP4_DNS=1.2.3.4")
	if b, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("%v: %s", e, b)
	}
	b, e := os.ReadFile(capture)
	if e != nil {
		t.Fatal(e)
	}
	if string(b) != "1|10.20.0.0|255.255.0.0|0|0|" {
		t.Fatal(string(b))
	}
	b, e = os.ReadFile(marker)
	if e != nil || string(b) != "connect|10.20.1.2|test" {
		t.Fatalf("marker %s %v", b, e)
	}
}
func TestUnixWrapperEnvironment(t *testing.T) {
	sh := "/bin/sh"
	if runtime.GOOS == "windows" {
		sh = "C:/Program Files/Git/bin/sh.exe"
		if _, e := os.Stat(sh); e != nil {
			t.Skip("no POSIX shell")
		}
	}
	d := t.TempDir()
	asShell := func(p string) string {
		p = filepath.ToSlash(p)
		if runtime.GOOS == "windows" {
			return "/" + strings.ToLower(p[:1]) + p[2:]
		}
		return p
	}
	stub := filepath.Join(d, "official ' space.sh")
	os.WriteFile(stub, []byte(`printf '%s|%s|%s|%s|%s|%s' "$CISCO_SPLIT_INC" "$CISCO_SPLIT_INC_0_ADDR" "$CISCO_SPLIT_INC_0_MASK" "$CISCO_SPLIT_EXC" "$CISCO_IPV6_SPLIT_INC" "$INTERNAL_IP4_DNS"
exit 0
`), 0700)
	marker := filepath.Join(d, "state")
	wrapper := filepath.Join(d, "wrapper.sh")
	os.WriteFile(wrapper, []byte(UnixScript(asShell(stub), asShell(marker), config.Default())), 0700)
	cmd := exec.Command(sh, asShell(wrapper))
	cmd.Env = append(os.Environ(), "reason=reconnect", "INTERNAL_IP4_ADDRESS=10.20.1.2", "TUNDEV=tun0", "INTERNAL_IP4_DNS=1.2.3.4", "CISCO_SPLIT_INC=55")
	b, e := cmd.CombinedOutput()
	if e != nil {
		t.Fatalf("%v: %s", e, b)
	}
	if string(b) != "1|10.20.0.0|255.255.0.0|0|0|" {
		t.Fatal(string(b))
	}
	b, e = os.ReadFile(marker)
	if e != nil || string(b) != "reconnect|10.20.1.2|tun0" {
		t.Fatalf("marker %s %v", b, e)
	}
}
func TestArgumentsAndRedaction(t *testing.T) {
	s := config.Default()
	s.Username = `name with "quotes"`
	args := Arguments(s, "path with spaces")
	b, _ := json.Marshal(args)
	if strings.Contains(string(b), "--no-cert-check") || strings.Contains(string(b), "--servercert") {
		t.Fatal(string(b))
	}
	for _, line := range []string{"COOKIE=private", "Set-Cookie: private", "Authorization: abc", "password=secret", "token: private"} {
		if got := Sanitize(line); got != "[已隐藏认证数据]" {
			t.Fatal(got)
		}
	}
}
