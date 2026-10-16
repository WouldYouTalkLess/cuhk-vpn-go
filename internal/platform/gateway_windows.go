package platform

import (
	"errors"
	"fmt"
	"net/netip"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

const gatewayRouteMetric uint32 = 42712

var ipHelper = windows.NewLazySystemDLL("iphlpapi.dll")
var initializeRoute = ipHelper.NewProc("InitializeIpForwardEntry")
var createRoute = ipHelper.NewProc("CreateIpForwardEntry2")
var deleteRoute = ipHelper.NewProc("DeleteIpForwardEntry2")

type gatewayAPI struct {
	routes   func() ([]windows.MibIpForwardRow2, error)
	iface    func(windows.MibIpForwardRow2) (windows.MibIfRow2, error)
	metric   func(windows.MibIpForwardRow2) (uint32, error)
	newRoute func() windows.MibIpForwardRow2
	create   func(*windows.MibIpForwardRow2) error
	delete   func(*windows.MibIpForwardRow2) error
}

func nativeGatewayAPI() gatewayAPI {
	return gatewayAPI{
		routes: ipv4Routes,
		iface: func(route windows.MibIpForwardRow2) (windows.MibIfRow2, error) {
			row := windows.MibIfRow2{InterfaceLuid: route.InterfaceLuid, InterfaceIndex: route.InterfaceIndex}
			err := windows.GetIfEntry2Ex(windows.MibIfEntryNormalWithoutStatistics, &row)
			return row, err
		},
		metric: func(route windows.MibIpForwardRow2) (uint32, error) {
			row := windows.MibIpInterfaceRow{Family: windows.AF_INET, InterfaceLuid: route.InterfaceLuid, InterfaceIndex: route.InterfaceIndex}
			err := windows.GetIpInterfaceEntry(&row)
			return row.Metric, err
		},
		newRoute: func() windows.MibIpForwardRow2 {
			var row windows.MibIpForwardRow2
			initializeRoute.Call(uintptr(unsafe.Pointer(&row)))
			return row
		},
		create: func(row *windows.MibIpForwardRow2) error {
			code, _, _ := createRoute.Call(uintptr(unsafe.Pointer(row)))
			if code != 0 {
				return windows.Errno(code)
			}
			return nil
		},
		delete: func(row *windows.MibIpForwardRow2) error {
			code, _, _ := deleteRoute.Call(uintptr(unsafe.Pointer(row)))
			if code != 0 {
				return windows.Errno(code)
			}
			return nil
		},
	}
}

func ipv4Routes() ([]windows.MibIpForwardRow2, error) {
	var table *windows.MibIpForwardTable2
	if err := windows.GetIpForwardTable2(windows.AF_INET, &table); err != nil {
		return nil, err
	}
	defer windows.FreeMibTable(unsafe.Pointer(table))
	return append([]windows.MibIpForwardRow2(nil), table.Rows()...), nil
}

func rawIPv4(addr netip.Addr) windows.RawSockaddrInet {
	var raw windows.RawSockaddrInet
	value := (*windows.RawSockaddrInet4)(unsafe.Pointer(&raw))
	value.Family = windows.AF_INET
	value.Addr = addr.As4()
	return raw
}

func ipv4Addr(raw windows.RawSockaddrInet) netip.Addr {
	if raw.Family != windows.AF_INET {
		return netip.Addr{}
	}
	return netip.AddrFrom4((*windows.RawSockaddrInet4)(unsafe.Pointer(&raw)).Addr)
}

// Use the same preference as Windows: route metric + interface metric. Hardware
// and operational flags exclude proxy/TUN/loopback interfaces even if those have
// a lower metric. The snapshot is refreshed for each connection, never cached.
func physicalGateway(api gatewayAPI, routes []windows.MibIpForwardRow2) (windows.MibIpForwardRow2, error) {
	var best windows.MibIpForwardRow2
	var bestMetric uint64
	found := false
	for _, route := range routes {
		addr := ipv4Addr(route.DestinationPrefix.Prefix)
		if route.DestinationPrefix.PrefixLength != 0 || !addr.IsValid() || !addr.IsUnspecified() || route.Loopback != 0 || !ipv4Addr(route.NextHop).IsValid() {
			continue
		}
		iface, err := api.iface(route)
		if errors.Is(err, windows.ERROR_NOT_FOUND) {
			continue // Adapter disappeared while the snapshot was being read.
		}
		if err != nil {
			return best, fmt.Errorf("读取网卡状态：%w", err)
		}
		const hardware, filter = 1, 2
		if iface.InterfaceAndOperStatusFlags&hardware == 0 || iface.InterfaceAndOperStatusFlags&filter != 0 || iface.OperStatus != windows.IfOperStatusUp || iface.TunnelType != 0 {
			continue
		}
		metric, err := api.metric(route)
		if errors.Is(err, windows.ERROR_NOT_FOUND) {
			continue
		}
		if err != nil {
			return best, fmt.Errorf("读取网卡跃点：%w", err)
		}
		total := uint64(route.Metric) + uint64(metric)
		if !found || total < bestMetric || (total == bestMetric && route.InterfaceIndex < best.InterfaceIndex) {
			best, bestMetric, found = route, total, true
		}
	}
	if !found {
		return best, errors.New("没有可用的物理网卡 IPv4 默认网关")
	}
	return best, nil
}

// GatewayInfo is a read-only snapshot for connection diagnostics.
type GatewayInfo struct {
	InterfaceIndex uint32 `json:"interfaceIndex"`
	NextHop        string `json:"nextHop"`
}

func PhysicalGateway() (GatewayInfo, error) {
	api := nativeGatewayAPI()
	routes, err := api.routes()
	if err != nil {
		return GatewayInfo{}, err
	}
	row, err := physicalGateway(api, routes)
	if err != nil {
		return GatewayInfo{}, err
	}
	return GatewayInfo{row.InterfaceIndex, ipv4Addr(row.NextHop).String()}, nil
}

func sameGateway(a, b windows.MibIpForwardRow2) bool {
	return a.InterfaceLuid == b.InterfaceLuid && a.InterfaceIndex == b.InterfaceIndex && ipv4Addr(a.NextHop) == ipv4Addr(b.NextHop)
}

func isHostRoute(row windows.MibIpForwardRow2, addr netip.Addr) bool {
	return row.DestinationPrefix.PrefixLength == 32 && ipv4Addr(row.DestinationPrefix.Prefix) == addr
}

// PinGateway is called only by the privileged helper. Existing host routes must
// agree with the physical path; only a successfully created route is owned and
// eligible for cleanup. There are no PowerShell processes or persistent routes.
func PinGateway(addr netip.Addr) (func() error, error) {
	return pinGateway(nativeGatewayAPI(), addr)
}

func pinGateway(api gatewayAPI, addr netip.Addr) (func() error, error) {
	nop := func() error { return nil }
	if !addr.Is4() || !addr.IsGlobalUnicast() {
		return nop, errors.New("VPN 网关必须是有效 IPv4 地址")
	}
	routes, err := api.routes()
	if err != nil {
		return nop, fmt.Errorf("读取 IPv4 路由：%w", err)
	}
	selected, err := physicalGateway(api, routes)
	if err != nil {
		return nop, err
	}
	existing := 0
	for _, route := range routes {
		if !isHostRoute(route, addr) {
			continue
		}
		existing++
		if !sameGateway(route, selected) {
			return nop, errors.New("已有 VPN 网关路由使用其他路径，已保留原路由")
		}
	}
	if existing > 1 {
		return nop, errors.New("VPN 网关存在多条主机路由，已保留原路由")
	}
	if existing == 1 {
		return nop, nil
	}
	row := api.newRoute()
	row.InterfaceLuid, row.InterfaceIndex = selected.InterfaceLuid, selected.InterfaceIndex
	row.DestinationPrefix = windows.IpAddressPrefix{Prefix: rawIPv4(addr), PrefixLength: 32}
	row.NextHop = selected.NextHop
	row.SitePrefixLength, row.Metric, row.Protocol = 32, gatewayRouteMetric, windows.MIB_IPPROTO_NETMGMT
	row.Loopback, row.AutoconfigureAddress, row.Publish, row.Immortal = 0, 0, 0, 0
	if err := api.create(&row); err != nil {
		// Do not claim an entry created by someone else in a concurrent race.
		return nop, fmt.Errorf("添加学校网关路由：%w", err)
	}
	var mu sync.Mutex
	owned := true
	return func() error {
		mu.Lock()
		defer mu.Unlock()
		if !owned {
			return nil
		}
		current, err := api.routes()
		if err != nil {
			return fmt.Errorf("读取待清理网关路由：%w", err)
		}
		for _, entry := range current {
			if !isHostRoute(entry, addr) || !sameGateway(entry, row) || entry.Metric != gatewayRouteMetric || entry.Protocol != row.Protocol {
				continue
			}
			if err := api.delete(&entry); err != nil && !errors.Is(err, windows.ERROR_NOT_FOUND) {
				return fmt.Errorf("清理学校网关路由：%w", err)
			}
		}
		owned = false
		return nil
	}, nil
}
