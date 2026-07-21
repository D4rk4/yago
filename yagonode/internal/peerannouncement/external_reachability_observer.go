package peerannouncement

import (
	"net/netip"

	"github.com/D4rk4/yago/yagomodel"
)

func externallyEligibleObserver(
	seed yagomodel.Seed,
	admitAddress func(netip.Addr) error,
) bool {
	if admitAddress == nil {
		return false
	}
	host, ok := seed.IP.Get()
	if !ok {
		return false
	}
	address, err := netip.ParseAddr(host.String())
	if err != nil {
		return false
	}

	return admitAddress(address) == nil
}
