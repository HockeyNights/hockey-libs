package grpcserver

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"strings"

	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
)

type TrustedProxies []netip.Prefix

func ParseTrustedProxies(values []string) (TrustedProxies, error) {
	trusted := make(TrustedProxies, 0, len(values))

	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}

		if prefix, err := netip.ParsePrefix(value); err == nil {
			trusted = append(trusted, prefix)
			continue
		}

		address, err := netip.ParseAddr(value)
		if err != nil {
			return nil, fmt.Errorf("grpcserver: trusted proxy %q is neither address nor CIDR: %w", value, err)
		}

		trusted = append(trusted, netip.PrefixFrom(address, address.BitLen()))
	}

	return trusted, nil
}

func (t TrustedProxies) Trust(address netip.Addr) bool {
	address = address.Unmap()

	for _, prefix := range t {
		if prefix.Contains(address) {
			return true
		}
	}

	return false
}

func ClientIP(ctx context.Context, trusted TrustedProxies) string {
	address, ok := peerAddr(ctx)
	if !ok {
		return ""
	}

	if !trusted.Trust(address) {
		return address.String()
	}

	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return address.String()
	}

	forwarded := metadataValue(md, "x-forwarded-for")
	if forwarded == "" {
		return address.String()
	}

	parts := strings.Split(forwarded, ",")
	for i := len(parts) - 1; i >= 0; i-- {
		hop, err := netip.ParseAddr(strings.TrimSpace(parts[i]))
		if err != nil {
			continue
		}
		if trusted.Trust(hop) {
			continue
		}

		return hop.Unmap().String()
	}

	return address.String()
}

func ClientAddr(ctx context.Context, trusted TrustedProxies) *netip.Addr {
	raw := ClientIP(ctx, trusted)
	if raw == "" {
		return nil
	}

	address, err := netip.ParseAddr(raw)
	if err != nil {
		return nil
	}

	address = address.Unmap()

	return &address
}

func peerAddr(ctx context.Context) (netip.Addr, bool) {
	p, ok := peer.FromContext(ctx)
	if !ok || p.Addr == nil {
		return netip.Addr{}, false
	}

	raw := p.Addr.String()
	if host, _, err := net.SplitHostPort(raw); err == nil {
		raw = host
	}

	address, err := netip.ParseAddr(raw)
	if err != nil {
		return netip.Addr{}, false
	}

	return address.Unmap(), true
}
