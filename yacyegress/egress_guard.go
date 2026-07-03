// Package yacyegress screens outbound connection targets by resolved IP address
// so a component never opens a connection to a non-public host. It backs SSRF
// protection for the node's peer traffic and the crawler's page fetches without
// an external forward proxy: the screening runs at dial time on the address the
// operating system is about to connect to, so a name that resolves to a public
// address at admission time cannot be rebound to a private one before the dial.
//
// Private networks are blocked by default because a public swarm peer or web
// origin never lives behind an RFC 1918 or unique-local address. Deployments on
// a LAN or a private YaCy network opt back in with a guard that allows private
// networks; loopback, link-local (including the cloud metadata range), carrier
// grade NAT, multicast, and reserved ranges stay blocked either way.
package yacyegress

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"syscall"
)

// ErrBlocked is the sentinel wrapped by every rejection so callers can classify
// a blocked target without matching on message text.
var ErrBlocked = errors.New("egress target is not a public address")

var blockedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("2001:db8::/32"),
}

// Guard admits or rejects an outbound target by its resolved IP address.
type Guard struct {
	allowPrivateNetworks   bool
	allowedPrivatePrefixes []netip.Prefix
}

// NewGuard builds a guard. When allowPrivateNetworks is true the guard permits
// RFC 1918 and unique-local addresses for LAN and private-network deployments.
// Options such as WithPrivateAllowlist narrow that trust to named ranges.
func NewGuard(allowPrivateNetworks bool, opts ...Option) Guard {
	guard := Guard{allowPrivateNetworks: allowPrivateNetworks}
	for _, opt := range opts {
		opt(&guard)
	}

	return guard
}

// AdmitAddr reports whether a connection to addr is allowed, returning an error
// wrapping ErrBlocked when it is not.
func (g Guard) AdmitAddr(addr netip.Addr) error {
	addr = addr.Unmap()
	if !addr.IsValid() {
		return fmt.Errorf("invalid address: %w", ErrBlocked)
	}
	if addr.IsPrivate() {
		if g.allowPrivateNetworks || g.allowlisted(addr) {
			return nil
		}
		return fmt.Errorf("private address %s: %w", addr, ErrBlocked)
	}
	if addr.IsLoopback() ||
		addr.IsLinkLocalUnicast() ||
		addr.IsLinkLocalMulticast() ||
		addr.IsMulticast() ||
		addr.IsUnspecified() {
		return fmt.Errorf("non-public address %s: %w", addr, ErrBlocked)
	}
	for _, prefix := range blockedPrefixes {
		if prefix.Contains(addr) {
			return fmt.Errorf("reserved address %s: %w", addr, ErrBlocked)
		}
	}
	return nil
}

// DialControl is a net.Dialer.Control hook that rejects a dial whose resolved
// address is not admitted. The dialer calls it with the concrete IP address the
// kernel is about to connect to, after name resolution.
func (g Guard) DialControl(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("split dial address %q: %w", address, err)
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return fmt.Errorf("parse dial address %q: %w", host, err)
	}
	return g.AdmitAddr(addr)
}
