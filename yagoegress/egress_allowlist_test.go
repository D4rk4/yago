package yagoegress_test

import (
	"errors"
	"net/netip"
	"testing"

	"github.com/D4rk4/yago/yagoegress"
)

func TestWithPrivateAllowlistAdmitsListedRange(t *testing.T) {
	guard := yagoegress.NewGuard(false, yagoegress.WithPrivateAllowlist(
		[]netip.Prefix{netip.MustParsePrefix("10.10.0.0/16")},
	))
	if err := guard.AdmitAddr(netip.MustParseAddr("10.10.5.5")); err != nil {
		t.Fatalf("AdmitAddr(allowlisted private) = %v, want nil", err)
	}
}

func TestWithPrivateAllowlistStillBlocksUnlistedPrivate(t *testing.T) {
	guard := yagoegress.NewGuard(false, yagoegress.WithPrivateAllowlist(
		[]netip.Prefix{netip.MustParsePrefix("10.10.0.0/16")},
	))
	for _, raw := range []string{"10.20.0.5", "192.168.1.5", "fc00::1"} {
		if err := guard.AdmitAddr(
			netip.MustParseAddr(raw),
		); !errors.Is(
			err,
			yagoegress.ErrBlocked,
		) {
			t.Errorf("AdmitAddr(%s) = %v, want ErrBlocked", raw, err)
		}
	}
}

func TestWithPrivateAllowlistDoesNotOpenReservedRanges(t *testing.T) {
	// A non-private prefix in the allowlist must never grant access to
	// loopback or the cloud metadata range.
	guard := yagoegress.NewGuard(false, yagoegress.WithPrivateAllowlist(
		[]netip.Prefix{
			netip.MustParsePrefix("169.254.0.0/16"),
			netip.MustParsePrefix("127.0.0.0/8"),
		},
	))
	for _, raw := range []string{"169.254.169.254", "127.0.0.1"} {
		if err := guard.AdmitAddr(
			netip.MustParseAddr(raw),
		); !errors.Is(
			err,
			yagoegress.ErrBlocked,
		) {
			t.Errorf("AdmitAddr(%s) = %v, want ErrBlocked", raw, err)
		}
	}
}
