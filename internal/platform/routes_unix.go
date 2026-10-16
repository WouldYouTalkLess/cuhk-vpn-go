//go:build darwin || linux

package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func VerifyRoutes(ctx context.Context, cidrs []string, ip, iface, tools string) error {
	nic, e := net.InterfaceByName(iface)
	if e != nil {
		return e
	}
	addrs, e := nic.Addrs()
	if e != nil {
		return e
	}
	hasIP := false
	for _, a := range addrs {
		p, e := netip.ParsePrefix(a.String())
		if e == nil && p.Addr().String() == ip {
			hasIP = true
		}
	}
	if !hasIP {
		return fmt.Errorf("VPN 地址 %s 尚未配置在 %s", ip, iface)
	}
	for _, cidr := range cidrs {
		prefix, e := netip.ParsePrefix(cidr)
		if e != nil {
			return e
		}
		address := prefix.Addr()
		if prefix.Bits() < 32 {
			address = address.Next()
		}
		if runtime.GOOS == "linux" {
			ipTool := "ip"
			if p := filepath.Join(tools, "ip"); tools != "" {
				if _, e := os.Stat(p); e == nil {
					ipTool = p
				}
			}
			b, e := exec.CommandContext(ctx, ipTool, "-j", "-4", "route", "get", address.String()).Output()
			if e != nil {
				return e
			}
			var routes []struct {
				Dev string `json:"dev"`
			}
			if e = json.Unmarshal(b, &routes); e != nil {
				return e
			}
			if len(routes) != 1 || routes[0].Dev != iface {
				return fmt.Errorf("%s 的路由未指向 %s", cidr, iface)
			}
		} else {
			b, e := exec.CommandContext(ctx, "/sbin/route", "-n", "get", "-inet", address.String()).Output()
			if e != nil {
				return e
			}
			found := false
			for _, line := range strings.Split(string(b), "\n") {
				f := strings.Fields(line)
				if len(f) == 2 && f[0] == "interface:" && f[1] == iface {
					found = true
				}
			}
			if !found {
				return fmt.Errorf("%s 的路由未指向 %s", cidr, iface)
			}
		}
	}
	return nil
}
