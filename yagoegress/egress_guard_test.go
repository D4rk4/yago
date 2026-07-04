package yagoegress_test

import (
	"errors"
	"net/netip"
	"testing"

	"github.com/D4rk4/yago/yagoegress"
)

func TestAdmitAddrPublicIsAllowed(t *testing.T) {
	guard := yagoegress.NewGuard(false)
	for _, raw := range []string{"8.8.8.8", "1.1.1.1", "2606:4700:4700::1111"} {
		if err := guard.AdmitAddr(netip.MustParseAddr(raw)); err != nil {
			t.Errorf("AdmitAddr(%s) = %v, want nil", raw, err)
		}
	}
}

func TestAdmitAddrBlocksNonPublic(t *testing.T) {
	guard := yagoegress.NewGuard(false)
	cases := []string{
		"127.0.0.1",       // loopback
		"::1",             // loopback v6
		"169.254.169.254", // link-local, cloud metadata
		"224.0.0.1",       // multicast
		"ff02::1",         // link-local multicast
		"0.0.0.0",         // unspecified
		"0.1.2.3",         // 0.0.0.0/8 reserved
		"100.64.0.1",      // carrier grade NAT
		"192.0.0.1",       // IETF protocol assignments
		"192.0.2.5",       // TEST-NET-1
		"198.18.0.1",      // benchmarking
		"198.51.100.5",    // TEST-NET-2
		"203.0.113.5",     // TEST-NET-3
		"240.0.0.1",       // reserved
		"100::1",          // IPv6 discard
		"2001:db8::1",     // IPv6 documentation
		"10.0.0.5",        // RFC 1918
		"172.16.0.5",      // RFC 1918
		"192.168.1.5",     // RFC 1918
		"fc00::1",         // unique local
	}
	for _, raw := range cases {
		err := guard.AdmitAddr(netip.MustParseAddr(raw))
		if !errors.Is(err, yagoegress.ErrBlocked) {
			t.Errorf("AdmitAddr(%s) = %v, want ErrBlocked", raw, err)
		}
	}
}

func TestAdmitAddrInvalid(t *testing.T) {
	if err := yagoegress.NewGuard(true).
		AdmitAddr(netip.Addr{}); !errors.Is(
		err,
		yagoegress.ErrBlocked,
	) {
		t.Fatalf("AdmitAddr(zero) = %v, want ErrBlocked", err)
	}
}

func TestAdmitAddrAllowsPrivateWhenEnabled(t *testing.T) {
	guard := yagoegress.NewGuard(true)
	for _, raw := range []string{"10.0.0.5", "172.16.0.5", "192.168.1.5", "fc00::1"} {
		if err := guard.AdmitAddr(netip.MustParseAddr(raw)); err != nil {
			t.Errorf("AdmitAddr(%s) with private allowed = %v, want nil", raw, err)
		}
	}
}

func TestAdmitAddrKeepsBlockingLoopbackWhenPrivateEnabled(t *testing.T) {
	guard := yagoegress.NewGuard(true)
	for _, raw := range []string{"127.0.0.1", "169.254.169.254", "100.64.0.1"} {
		if err := guard.AdmitAddr(
			netip.MustParseAddr(raw),
		); !errors.Is(
			err,
			yagoegress.ErrBlocked,
		) {
			t.Errorf("AdmitAddr(%s) with private allowed = %v, want ErrBlocked", raw, err)
		}
	}
}

func TestDialControlAllowsPublic(t *testing.T) {
	if err := yagoegress.NewGuard(false).DialControl("tcp", "8.8.8.8:443", nil); err != nil {
		t.Fatalf("DialControl(public) = %v, want nil", err)
	}
}

func TestDialControlBlocksPrivate(t *testing.T) {
	if err := yagoegress.NewGuard(false).
		DialControl("tcp", "127.0.0.1:80", nil); !errors.Is(
		err,
		yagoegress.ErrBlocked,
	) {
		t.Fatalf("DialControl(loopback) = %v, want ErrBlocked", err)
	}
}

func TestDialControlRejectsMalformedAddress(t *testing.T) {
	if err := yagoegress.NewGuard(false).DialControl("tcp", "8.8.8.8", nil); err == nil {
		t.Fatal("DialControl(no port) = nil, want error")
	}
}

func TestDialControlRejectsUnresolvedHost(t *testing.T) {
	if err := yagoegress.NewGuard(false).DialControl("tcp", "example.com:80", nil); err == nil {
		t.Fatal("DialControl(hostname) = nil, want error")
	}
}
