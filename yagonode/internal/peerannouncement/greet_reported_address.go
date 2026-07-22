package peerannouncement

import (
	"net/netip"
	"strings"

	"github.com/D4rk4/yago/yagomodel"
)

func validGreetReportedAddresses(value string, self yagomodel.Seed) bool {
	for _, raw := range strings.Split(value, ",") {
		candidate := strings.TrimSpace(raw)
		address, err := netip.ParseAddr(candidate)
		if err == nil {
			if address.IsValid() && !address.Unmap().IsUnspecified() {
				return true
			}

			continue
		}
		if matchesAdvertisedSelfHostname(candidate, self) {
			return true
		}
	}

	return false
}

func matchesAdvertisedSelfHostname(candidate string, self yagomodel.Seed) bool {
	reported, ok := canonicalHostname(candidate)
	if !ok {
		return false
	}
	advertised, ok := self.IP.Get()
	if !ok {
		return false
	}
	configured, ok := canonicalHostname(advertised.String())
	if !ok {
		return false
	}

	return reported == configured
}

func canonicalHostname(value string) (string, bool) {
	value = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(value)), ".")
	parsed, err := yagomodel.ParseHost(value)
	if err != nil {
		return "", false
	}
	if _, err := netip.ParseAddr(parsed.String()); err == nil {
		return "", false
	}

	return parsed.String(), true
}
