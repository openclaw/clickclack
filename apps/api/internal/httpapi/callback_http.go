package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"time"
)

const callbackTimeout = 3 * time.Second

// These ranges are globally routable in Go's broad sense but are reserved for
// protocols, documentation, benchmarking, or other special-purpose use. Keep
// this policy aligned with the IANA IPv4 and IPv6 special-purpose registries.
var blockedCallbackPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.31.196.0/24"),
	netip.MustParsePrefix("192.52.193.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("192.175.48.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("2620:4f:8000::/48"),
	netip.MustParsePrefix("3ffe::/16"),
	netip.MustParsePrefix("3fff::/20"),
}

// Public IPv6 unicast allocations currently come from 2000::/3. Rejecting
// other space by default keeps future or transition ranges closed until their
// callback safety is understood.
var publicIPv6Prefix = netip.MustParsePrefix("2000::/3")

type callbackDialer struct {
	lookupNetIP func(context.Context, string, string) ([]netip.Addr, error)
	dialContext func(context.Context, string, string) (net.Conn, error)
}

func (d *callbackDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if network != "tcp" && network != "tcp4" && network != "tcp6" {
		return nil, fmt.Errorf("callback network %q is not allowed", network)
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("invalid callback address: %w", err)
	}
	var addresses []netip.Addr
	if literal, parseErr := netip.ParseAddr(host); parseErr == nil {
		addresses = []netip.Addr{literal}
	} else {
		addresses, err = d.lookupNetIP(ctx, "ip", host)
		if err != nil {
			return nil, fmt.Errorf("resolve callback host: %w", err)
		}
	}
	if len(addresses) == 0 {
		return nil, errors.New("callback host resolved to no addresses")
	}
	for _, address := range addresses {
		if !isPublicCallbackAddr(address) {
			return nil, errors.New("callback host resolves to a non-public address")
		}
	}
	var dialErrors []error
	for _, address := range addresses {
		conn, dialErr := d.dialContext(ctx, network, net.JoinHostPort(address.String(), port))
		if dialErr == nil {
			return conn, nil
		}
		dialErrors = append(dialErrors, dialErr)
	}
	return nil, fmt.Errorf("connect to callback host: %w", errors.Join(dialErrors...))
}

func isPublicCallbackAddr(address netip.Addr) bool {
	if address.Zone() != "" {
		return false
	}
	address = address.Unmap()
	if !address.IsValid() || !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() || address.IsMulticast() || address.IsUnspecified() {
		return false
	}
	if address.Is6() && !publicIPv6Prefix.Contains(address) {
		return false
	}
	for _, prefix := range blockedCallbackPrefixes {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}
