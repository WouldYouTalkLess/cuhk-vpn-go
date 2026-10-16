package platform

import (
	"encoding/json"
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func gatewayRow(prefix string, index, metric uint32) windows.MibIpForwardRow2 {
	p := netip.MustParsePrefix(prefix)
	return windows.MibIpForwardRow2{
		InterfaceLuid: uint64(index) << 24, InterfaceIndex: index,
		DestinationPrefix: windows.IpAddressPrefix{Prefix: rawIPv4(p.Addr()), PrefixLength: uint8(p.Bits())},
		NextHop:           rawIPv4(netip.MustParseAddr("192.168.1.1")), Metric: metric, Protocol: windows.MIB_IPPROTO_NETMGMT,
	}
}

type gatewayFixture struct {
	rows      []windows.MibIpForwardRow2
	created   []windows.MibIpForwardRow2
	deleted   []windows.MibIpForwardRow2
	createErr error
	deleteErr error
}

func (f *gatewayFixture) api() gatewayAPI {
	return gatewayAPI{
		routes: func() ([]windows.MibIpForwardRow2, error) {
			return append([]windows.MibIpForwardRow2(nil), f.rows...), nil
		},
		iface: func(row windows.MibIpForwardRow2) (windows.MibIfRow2, error) {
			return windows.MibIfRow2{InterfaceIndex: row.InterfaceIndex, InterfaceAndOperStatusFlags: 1, OperStatus: windows.IfOperStatusUp}, nil
		},
		metric: func(windows.MibIpForwardRow2) (uint32, error) { return 10, nil },
		newRoute: func() windows.MibIpForwardRow2 {
			return windows.MibIpForwardRow2{ValidLifetime: ^uint32(0), PreferredLifetime: ^uint32(0)}
		},
		create: func(row *windows.MibIpForwardRow2) error {
			f.created = append(f.created, *row)
			if f.createErr == nil {
				f.rows = append(f.rows, *row)
			}
			return f.createErr
		},
		delete: func(row *windows.MibIpForwardRow2) error {
			f.deleted = append(f.deleted, *row)
			if f.deleteErr != nil {
				return f.deleteErr
			}
			for i, existing := range f.rows {
				if existing == *row {
					f.rows = append(f.rows[:i], f.rows[i+1:]...)
					break
				}
			}
			return nil
		},
	}
}

func TestPhysicalGatewaySelection(t *testing.T) {
	f := gatewayFixture{rows: []windows.MibIpForwardRow2{
		gatewayRow("0.0.0.0/0", 1, 0), // Virtual TUN, lowest metric.
		gatewayRow("0.0.0.0/0", 2, 0), // Disconnected hardware.
		gatewayRow("0.0.0.0/0", 3, 30),
		gatewayRow("0.0.0.0/0", 4, 1),
		gatewayRow("10.20.0.0/16", 5, 0),
	}}
	api := f.api()
	api.iface = func(row windows.MibIpForwardRow2) (windows.MibIfRow2, error) {
		info := windows.MibIfRow2{InterfaceAndOperStatusFlags: 1, OperStatus: windows.IfOperStatusUp}
		if row.InterfaceIndex == 1 {
			info.InterfaceAndOperStatusFlags = 0
		}
		if row.InterfaceIndex == 2 {
			info.OperStatus = windows.IfOperStatusDown
		}
		return info, nil
	}
	api.metric = func(row windows.MibIpForwardRow2) (uint32, error) {
		if row.InterfaceIndex == 4 {
			return 60, nil
		}
		return 5, nil
	}
	selected, err := physicalGateway(api, f.rows)
	if err != nil || selected.InterfaceIndex != 3 {
		t.Fatalf("selected=%d err=%v", selected.InterfaceIndex, err)
	}
	api.metric = func(row windows.MibIpForwardRow2) (uint32, error) {
		if row.InterfaceIndex == 4 {
			return 34, nil
		}
		return 5, nil
	}
	selected, err = physicalGateway(api, f.rows)
	if err != nil || selected.InterfaceIndex != 3 {
		t.Fatalf("tie selection=%d err=%v", selected.InterfaceIndex, err)
	}
	api.iface = func(windows.MibIpForwardRow2) (windows.MibIfRow2, error) { return windows.MibIfRow2{}, nil }
	if _, err := physicalGateway(api, f.rows); err == nil {
		t.Fatal("accepted virtual default route")
	}
}

func TestPinGatewayPreservesExistingRoutes(t *testing.T) {
	addr := netip.MustParseAddr("192.0.2.77")
	for _, name := range []string{"same-path", "different-path", "multiple", "create-race"} {
		t.Run(name, func(t *testing.T) {
			f := gatewayFixture{rows: []windows.MibIpForwardRow2{gatewayRow("0.0.0.0/0", 3, 20)}}
			switch name {
			case "same-path":
				f.rows = append(f.rows, gatewayRow("192.0.2.77/32", 3, 99))
			case "different-path":
				f.rows = append(f.rows, gatewayRow("192.0.2.77/32", 4, 99))
			case "multiple":
				f.rows = append(f.rows, gatewayRow("192.0.2.77/32", 3, 99), gatewayRow("192.0.2.77/32", 3, 98))
			case "create-race":
				f.createErr = windows.ERROR_OBJECT_ALREADY_EXISTS
			}
			cleanup, err := pinGateway(f.api(), addr)
			if (err == nil) != (name == "same-path") {
				t.Fatalf("err=%v", err)
			}
			if err := cleanup(); err != nil {
				t.Fatal(err)
			}
			if len(f.deleted) != 0 {
				t.Fatal("deleted a pre-existing/foreign route")
			}
			if name != "create-race" && len(f.created) != 0 {
				t.Fatal("modified an existing route")
			}
		})
	}
}

func TestPinGatewayOwnedCleanup(t *testing.T) {
	addr := netip.MustParseAddr("192.0.2.77")
	for _, name := range []string{"remove", "retry", "changed-metric", "changed-interface", "gone"} {
		t.Run(name, func(t *testing.T) {
			f := gatewayFixture{rows: []windows.MibIpForwardRow2{gatewayRow("0.0.0.0/0", 3, 20)}}
			cleanup, err := pinGateway(f.api(), addr)
			if err != nil {
				t.Fatal(err)
			}
			created := f.created[0]
			if !isHostRoute(created, addr) || created.Metric != gatewayRouteMetric || created.Loopback != 0 || created.Publish != 0 || created.Protocol != windows.MIB_IPPROTO_NETMGMT {
				t.Fatal("invalid owned route")
			}
			switch name {
			case "retry":
				f.deleteErr = windows.ERROR_ACCESS_DENIED
			case "changed-metric":
				f.rows[1].Metric = 99
			case "changed-interface":
				f.rows[1].InterfaceLuid++
			case "gone":
				f.rows = f.rows[:1]
			}
			if name == "retry" {
				if err := cleanup(); !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
					t.Fatalf("cleanup err=%v", err)
				}
				f.deleteErr = nil
			}
			if err := cleanup(); err != nil {
				t.Fatal(err)
			}
			deletes := len(f.deleted)
			if name == "remove" && deletes != 1 || name == "retry" && deletes != 2 || name != "remove" && name != "retry" && deletes != 0 {
				t.Fatalf("unexpected deletions: %d", deletes)
			}
			if err := cleanup(); err != nil {
				t.Fatal(err)
			}
			if len(f.deleted) != deletes {
				t.Fatal("cleanup not idempotent")
			}
		})
	}
}

// Opt-in real-machine checks, separate from the deterministic CI tests. The
// profile only reads; the elevated lifecycle check temporarily pins a reserved
// TEST-NET address, never the school's real gateway or a default route.
func TestNativeGatewayProfile(t *testing.T) {
	output := os.Getenv("CUHK_GATEWAY_PROFILE")
	if output == "" {
		t.Skip("set CUHK_GATEWAY_PROFILE to a report path")
	}
	type sample struct {
		Milliseconds float64     `json:"ms"`
		Gateway      GatewayInfo `json:"gateway"`
	}
	samples := []sample{}
	for i := 0; i < 10; i++ {
		start := time.Now()
		info, err := PhysicalGateway()
		if err != nil {
			t.Fatal(err)
		}
		samples = append(samples, sample{float64(time.Since(start).Microseconds()) / 1000, info})
	}
	b, _ := json.MarshalIndent(samples, "", "  ")
	if err := os.WriteFile(output, b, 0600); err != nil {
		t.Fatal(err)
	}
	t.Log(string(b))
}

func TestNativeGatewayLifecycle(t *testing.T) {
	output := os.Getenv("CUHK_GATEWAY_LIFECYCLE")
	if output == "" {
		t.Skip("set CUHK_GATEWAY_LIFECYCLE to a report path and run elevated")
	}
	type report struct {
		Created  bool    `json:"created"`
		Reused   bool    `json:"reused"`
		Removed  bool    `json:"removed"`
		AddMs    float64 `json:"addMs"`
		RemoveMs float64 `json:"removeMs"`
		Error    string  `json:"error,omitempty"`
	}
	r := report{}
	defer func() { b, _ := json.MarshalIndent(r, "", "  "); os.WriteFile(filepath.Clean(output), b, 0600) }()
	if !Privileged() {
		r.Error = "Elevated Windows token required"
		t.Fatal(r.Error)
	}
	addr := netip.MustParseAddr("192.0.2.77")
	before, err := ipv4Routes()
	if err != nil {
		r.Error = err.Error()
		t.Fatal(err)
	}
	for _, row := range before {
		if isHostRoute(row, addr) {
			r.Error = "Test address already has a host route; no changes made"
			t.Fatal(r.Error)
		}
	}
	start := time.Now()
	cleanup, err := PinGateway(addr)
	r.AddMs = float64(time.Since(start).Microseconds()) / 1000
	if err != nil {
		r.Error = err.Error()
		t.Fatal(err)
	}
	defer func() {
		if err := cleanup(); err != nil {
			r.Error = err.Error()
			t.Error(err)
		}
	}()
	rows, err := ipv4Routes()
	if err != nil {
		r.Error = err.Error()
		t.Fatal(err)
	}
	for _, row := range rows {
		if isHostRoute(row, addr) && row.Metric == gatewayRouteMetric {
			r.Created = true
		}
	}
	if !r.Created {
		r.Error = "Created route not found"
		t.Fatal(r.Error)
	}
	reusedCleanup, err := PinGateway(addr)
	if err != nil {
		r.Error = err.Error()
		t.Fatal(err)
	}
	if err := reusedCleanup(); err != nil {
		r.Error = err.Error()
		t.Fatal(err)
	}
	rows, err = ipv4Routes()
	if err != nil {
		r.Error = err.Error()
		t.Fatal(err)
	}
	for _, row := range rows {
		if isHostRoute(row, addr) {
			r.Reused = true
		}
	}
	if !r.Reused {
		r.Error = "Reusing a route removed the original owner's entry"
		t.Fatal(r.Error)
	}
	start = time.Now()
	if err := cleanup(); err != nil {
		r.Error = err.Error()
		t.Fatal(err)
	}
	r.RemoveMs = float64(time.Since(start).Microseconds()) / 1000
	rows, err = ipv4Routes()
	if err != nil {
		r.Error = err.Error()
		t.Fatal(err)
	}
	r.Removed = true
	for _, row := range rows {
		if isHostRoute(row, addr) {
			r.Removed = false
		}
	}
	if !r.Removed {
		r.Error = "Owned test route remained after cleanup"
		t.Fatal(r.Error)
	}
}
