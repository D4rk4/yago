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
	host, hostKnown := caller.IP.Get()
	_, portKnown := caller.Port.Get()
	if !hostKnown || !portKnown {
		return false
	}
	address, err := netip.ParseAddr(host.String())

	return err != nil || !address.Unmap().IsUnspecified()
}
