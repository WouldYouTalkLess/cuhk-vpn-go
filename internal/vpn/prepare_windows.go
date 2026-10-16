package vpn

import (
	"context"
	"cuhk-vpn-go/internal/config"
	"cuhk-vpn-go/internal/platform"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"time"
)

// Pin the gateway through the physical interface before authentication. This also
// avoids a route switch when another application's TUN interface is present.
func prepare(s config.Settings, dir string) ([]string, []string, func() error, error) {
	nop := func() error { return nil }
	u, _ := url.Parse(s.Server)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	ips, e := net.DefaultResolver.LookupNetIP(ctx, "ip4", u.Hostname())
	if e != nil {
		return nil, nil, nop, e
	}
	var selected netip.Addr
	for _, ip := range ips {
		if ip.IsGlobalUnicast() && !netip.MustParsePrefix("198.18.0.0/15").Contains(ip) {
			selected = ip
			break
		}
	}
	if !selected.IsValid() {
		return nil, nil, nop, fmt.Errorf("VPN 网关没有真实 IPv4 地址，请检查 DNS 或代理的 Fake-IP 排除设置")
	}
	cleanup, e := platform.PinGateway(selected)
	if e != nil {
		return nil, nil, cleanup, e
	}
	return []string{"--resolve", u.Hostname() + ":" + selected.String(), "--interface", "CUHK VPN Go"}, []string{"CUHK_VPN_MANAGED_GATEWAY=" + selected.String()}, cleanup, nil
}
