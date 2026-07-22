package yagomodel

import (
	"net/netip"
	"strings"
)

const maximumAdvertisedHosts = 5

func (s Seed) AdvertisedHosts() []Host {
	hosts := make([]Host, 0, maximumAdvertisedHosts)
	seen := make(map[string]struct{}, maximumAdvertisedHosts)
	appendHost := func(host Host) {
		if len(hosts) == maximumAdvertisedHosts || host.String() == "" {
			return
		}
		host = canonicalAdvertisedHost(host)
		identity := host.String()
		if _, duplicate := seen[identity]; duplicate {
			return
		}
		seen[identity] = struct{}{}
		hosts = append(hosts, host)
	}

	if primary, ok := s.IP.Get(); ok {
		appendHost(primary)
	}
	if alternatives, ok := s.IP6.Get(); ok {
		for _, alternative := range alternatives {
			appendHost(alternative)
			if len(hosts) == maximumAdvertisedHosts {
				break
			}
		}
	}

	return hosts
}

func (s Seed) WithPrimaryHost(primary Host) Seed {
	promoted := s.Copy()
	primary = canonicalAdvertisedHost(primary)
	promoted.IP = Some(primary)
	secondary := make([]Host, 0)
	seen := map[string]struct{}{advertisedHostIdentity(primary): {}}
	appendSecondary := func(host Host) {
		identity := advertisedHostIdentity(host)
		if _, duplicate := seen[identity]; duplicate {
			return
		}
		if _, err := netip.ParseAddr(host.String()); err != nil {
			return
		}
		seen[identity] = struct{}{}
		secondary = append(secondary, host)
	}

	if previous, ok := s.IP.Get(); ok {
		appendSecondary(previous)
	}
	if alternatives, ok := s.IP6.Get(); ok {
		for _, alternative := range alternatives {
			appendSecondary(alternative)
		}
	}
	if len(secondary) == 0 {
		promoted.IP6 = None[[]Host]()
	} else {
		promoted.IP6 = Some(secondary)
	}

	return promoted
}

func advertisedHostIdentity(host Host) string {
	return canonicalAdvertisedHost(host).String()
}

func canonicalAdvertisedHost(host Host) Host {
	if host.addr.IsValid() {
		return Host{addr: host.addr.Unmap()}
	}

	return Host{hostname: strings.ToLower(strings.TrimSuffix(host.hostname, "."))}
}
