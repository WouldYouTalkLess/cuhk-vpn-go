package platform

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func VerifyRoutes(ctx context.Context, cidrs []string, ip, iface, tools string) error {
	const code = `$ErrorActionPreference='Stop';$i=@(Get-NetIPAddress -AddressFamily IPv4|Where-Object IPAddress -eq $env:CUHK_CHECK_IP);if($i.Count -ne 1){throw 'VPN IPv4 address is not assigned uniquely'};foreach($prefix in ($env:CUHK_CHECK_ROUTES -split ',')){$r=@(Get-NetRoute -AddressFamily IPv4 -DestinationPrefix $prefix -PolicyStore ActiveStore|Where-Object InterfaceIndex -eq $i[0].InterfaceIndex);if($r.Count -eq 0){throw ('Missing VPN route: '+$prefix)}}`
	cmd := exec.CommandContext(ctx, filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe"), "-NoProfile", "-NonInteractive", "-Command", code)
	cmd.Env = append(os.Environ(), "CUHK_CHECK_IP="+ip, "CUHK_CHECK_ROUTES="+strings.Join(cidrs, ","))
	hide(cmd)
	if b, e := cmd.CombinedOutput(); e != nil {
		return CommandError("分流路由检查", e, b)
	}
	return nil
}
