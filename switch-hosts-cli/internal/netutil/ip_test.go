package netutil

import (
	"net"
	"testing"
)

func TestIPv4StringsWithinNetworkFiltersAndSorts(t *testing.T) {
	_, network, err := net.ParseCIDR("192.168.1.0/24")
	if err != nil {
		t.Fatalf("ParseCIDR() error = %v", err)
	}

	addrs := []net.Addr{
		&net.IPNet{IP: net.ParseIP("192.168.1.99"), Mask: network.Mask},
		&net.IPNet{IP: net.ParseIP("10.0.0.2"), Mask: net.CIDRMask(24, 32)},
		&net.IPNet{IP: net.ParseIP("192.168.1.48"), Mask: network.Mask},
		&net.IPNet{IP: net.ParseIP("192.168.1.48"), Mask: network.Mask},
		&net.IPNet{IP: net.ParseIP("fe80::1"), Mask: net.CIDRMask(64, 128)},
	}

	got := IPv4StringsWithinNetwork(addrs, network)
	expected := []string{"192.168.1.48", "192.168.1.99"}

	if len(got) != len(expected) {
		t.Fatalf("IPv4StringsWithinNetwork() len = %d, want %d; values=%v", len(got), len(expected), got)
	}

	for i := range expected {
		if got[i] != expected[i] {
			t.Fatalf("IPv4StringsWithinNetwork()[%d] = %q, want %q", i, got[i], expected[i])
		}
	}
}
