package peeradmission

import (
	"context"
	"net/netip"
	"strings"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/httpguard"
)

func observableHelloCaller(
	ctx context.Context,
	caller yagomodel.Seed,
) (yagomodel.Seed, bool) {
	if observableAdvertisedCaller(caller) {
		return caller, true
	}
	if _, advertisedPort := caller.Port.Get(); !advertisedPort {
		return caller, false
	}

	address, err := netip.ParseAddr(strings.TrimSpace(httpguard.RemoteAddr(ctx)))
	if err != nil || address.Unmap().IsUnspecified() {
		return caller, false
	}
	host, _ := yagomodel.ParseHost(address.String())

	observed := caller.Copy()
	observed.IP = yagomodel.Some(host)

	return observed, true
}

func observableAdvertisedCaller(caller yagomodel.Seed) bool {
	_, portKnown := caller.Port.Get()
	if !portKnown {
		return false
	}
	for _, host := range caller.AdvertisedHosts() {
		address, err := netip.ParseAddr(host.String())
		if err != nil || !address.Unmap().IsUnspecified() {
			return true
		}
	}

	return false
}
