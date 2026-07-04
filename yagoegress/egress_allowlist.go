package yagoegress

import "net/netip"

// Option adjusts a Guard at construction time.
type Option func(*Guard)

// WithPrivateAllowlist admits private addresses that fall inside one of the
// given prefixes even when private networks are otherwise blocked, so an
// intranet deployment can reach named internal ranges without opening all of
// RFC 1918 and unique-local space. The allowlist only relaxes the private-range
// check: loopback, link-local (including the cloud metadata range), carrier
// grade NAT, multicast, and reserved ranges stay blocked, so a non-private
// prefix in the list never grants access to those ranges.
func WithPrivateAllowlist(prefixes []netip.Prefix) Option {
	return func(g *Guard) {
		g.allowedPrivatePrefixes = prefixes
	}
}

func (g Guard) allowlisted(addr netip.Addr) bool {
	for _, prefix := range g.allowedPrivatePrefixes {
		if prefix.Contains(addr) {
			return true
		}
	}

	return false
}
