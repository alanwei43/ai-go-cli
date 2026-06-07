package netutil

import (
	"fmt"
	"net"
	"sort"
)

func LocalIPv4StringsWithinCIDR(cidr string) ([]string, error) {
	_, network, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, fmt.Errorf("parse ip prefix: %w", err)
	}

	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil, fmt.Errorf("list interface addresses: %w", err)
	}

	return IPv4StringsWithinNetwork(addrs, network), nil
}

func IPv4StringsWithinNetwork(addrs []net.Addr, network *net.IPNet) []string {
	seen := map[string]struct{}{}
	var ips []string

	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok || ipNet == nil {
			continue
		}

		ip := ipNet.IP.To4()
		if ip == nil || !network.Contains(ip) {
			continue
		}

		text := ip.String()
		if _, exists := seen[text]; exists {
			continue
		}

		seen[text] = struct{}{}
		ips = append(ips, text)
	}

	sort.Strings(ips)
	return ips
}
