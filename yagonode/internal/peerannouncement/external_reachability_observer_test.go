package peerannouncement

import (
	"errors"
	"net/netip"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
)

func admitObserverAddresses(values ...string) func(netip.Addr) error {
	addresses := make(map[netip.Addr]struct{}, len(values))
	for _, value := range values {
		addresses[netip.MustParseAddr(value)] = struct{}{}
	}

	return func(address netip.Addr) error {
		if _, admitted := addresses[address]; !admitted {
			return errors.New("observer address rejected")
		}

		return nil
	}
}

func TestExternallyEligibleObserverAcceptsPublicLiteralAddresses(t *testing.T) {
	for _, address := range []string{"1.1.1.1", "2606:4700:4700::1111"} {
		t.Run(address, func(t *testing.T) {
			if !externallyEligibleObserver(
				callerSeed(t, "peer", address),
				admitObserverAddresses(address),
			) {
				t.Fatalf("public literal observer %s was rejected", address)
			}
		})
	}
}

func TestExternallyEligibleObserverFailsClosedWithoutAddressAdmission(t *testing.T) {
	if externallyEligibleObserver(callerSeed(t, "peer", "1.1.1.1"), nil) {
		t.Fatal("observer was accepted without an address admission policy")
	}
}

func TestExternallyEligibleObserverHonorsAddressAdmissionRejection(t *testing.T) {
	if externallyEligibleObserver(
		callerSeed(t, "peer", "1.1.1.1"),
		admitObserverAddresses(),
	) {
		t.Fatal("observer rejected by the address admission policy was accepted")
	}
}

func TestExternallyEligibleObserverRejectsUndialedIPv6OnlyAddress(t *testing.T) {
	hosts, err := yagomodel.ParseIP6("2606:4700:4700::1111")
	if err != nil {
		t.Fatalf("ParseIP6: %v", err)
	}
	seed := yagomodel.Seed{
		Hash: hashFor("peer"),
		IP6:  yagomodel.Some(hosts),
		Port: yagomodel.Some(yagomodel.Port(seedPort)),
	}

	if externallyEligibleObserver(
		seed,
		admitObserverAddresses("2606:4700:4700::1111"),
	) {
		t.Fatal("undialed IPv6-only observer was accepted")
	}
}

func TestExternallyEligibleObserverRejectsHostnameWithoutVerifiedDialAddress(t *testing.T) {
	if externallyEligibleObserver(
		callerSeed(t, "peer", "public.example"),
		admitObserverAddresses("1.1.1.1"),
	) {
		t.Fatal("hostname observer without a verified dial address was accepted")
	}
}
