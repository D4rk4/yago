package yagoegress

import (
	"context"
	"net"
	"net/netip"
)

// DialContext opens a connection to an address, matching net.Dialer.DialContext
// so it composes with net/http transports.
type DialContext func(ctx context.Context, network, address string) (net.Conn, error)

// HostResolver returns the IP addresses a host name resolves to.
type HostResolver func(ctx context.Context, host string) ([]netip.Addr, error)

// PreferIPv4 wraps a dial so a host that resolves to both address families is
// tried over IPv4 first and only falls back to IPv6. A host with IPv6 disabled
// cannot even create an IPv6 socket ("address family not supported by protocol"),
// so a peer or origin that also has an A record must be reached over it; the
// default IPv6-first ordering would fail such a host outright. A literal IP
// address, an unparseable address, or a resolver error passes straight through to
// the wrapped dial. resolve is injectable for tests; nil uses the system resolver.
func PreferIPv4(resolve HostResolver, dial DialContext) DialContext {
	if resolve == nil {
		resolve = systemHostResolver
	}

	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return dial(ctx, network, address)
		}
		if _, err := netip.ParseAddr(host); err == nil {
			return dial(ctx, network, address)
		}
		addresses, err := resolve(ctx, host)
		if err != nil || len(addresses) == 0 {
			return dial(ctx, network, address)
		}

		return dialInIPv4FirstOrder(ctx, network, port, addresses, dial)
	}
}

func dialInIPv4FirstOrder(
	ctx context.Context,
	network, port string,
	addresses []netip.Addr,
	dial DialContext,
) (net.Conn, error) {
	var firstErr error
	for _, addr := range ipv4First(addresses) {
		conn, err := dial(ctx, network, net.JoinHostPort(addr.Unmap().String(), port))
		if err == nil {
			return conn, nil
		}
		if firstErr == nil {
			firstErr = err
		}
	}

	return nil, firstErr
}

func ipv4First(addresses []netip.Addr) []netip.Addr {
	ordered := make([]netip.Addr, 0, len(addresses))
	for _, addr := range addresses {
		if addr.Unmap().Is4() {
			ordered = append(ordered, addr)
		}
	}
	for _, addr := range addresses {
		if !addr.Unmap().Is4() {
			ordered = append(ordered, addr)
		}
	}

	return ordered
}

func systemHostResolver(ctx context.Context, host string) ([]netip.Addr, error) {
	//nolint:wrapcheck // thin adapter over the system resolver; its lookup error is self-describing and PreferIPv4 falls back to a raw dial on it.
	return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
}
