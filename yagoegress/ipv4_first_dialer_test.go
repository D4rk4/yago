package yagoegress_test

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"testing"

	"github.com/D4rk4/yago/yagoegress"
)

func recordingDial(dialed *[]string, fail map[string]error) yagoegress.DialContext {
	return func(_ context.Context, _, address string) (net.Conn, error) {
		*dialed = append(*dialed, address)
		if err, ok := fail[address]; ok {
			return nil, err
		}
		return nil, nil
	}
}

func staticResolver(addrs ...string) yagoegress.HostResolver {
	return func(context.Context, string) ([]netip.Addr, error) {
		parsed := make([]netip.Addr, 0, len(addrs))
		for _, a := range addrs {
			parsed = append(parsed, netip.MustParseAddr(a))
		}
		return parsed, nil
	}
}

func TestPreferIPv4DialsIPv4BeforeIPv6(t *testing.T) {
	var dialed []string
	dial := yagoegress.PreferIPv4(
		staticResolver("2001:db8::1", "203.0.113.7"),
		recordingDial(&dialed, nil),
	)

	if _, err := dial(context.Background(), "tcp", "peer.example:8090"); err != nil {
		t.Fatalf("dial: %v", err)
	}
	if len(dialed) != 1 || dialed[0] != "203.0.113.7:8090" {
		t.Fatalf("dialed = %v, want the IPv4 address first", dialed)
	}
}

func TestPreferIPv4FallsBackToIPv6WhenIPv4Fails(t *testing.T) {
	var dialed []string
	v4Down := map[string]error{"203.0.113.7:8090": errors.New("ipv4 unreachable")}
	dial := yagoegress.PreferIPv4(
		staticResolver("203.0.113.7", "2001:db8::1"),
		recordingDial(&dialed, v4Down),
	)

	if _, err := dial(context.Background(), "tcp", "peer.example:8090"); err != nil {
		t.Fatalf("dial: %v", err)
	}
	want := []string{"203.0.113.7:8090", "2001:db8::1:8090"}
	if len(dialed) != 2 || dialed[0] != want[0] ||
		dialed[1] != net.JoinHostPort("2001:db8::1", "8090") {
		t.Fatalf("dialed = %v, want IPv4 then IPv6 fallback", dialed)
	}
}

func TestPreferIPv4ReturnsFirstErrorWhenAllFail(t *testing.T) {
	var dialed []string
	firstErr := errors.New("ipv4 refused")
	fail := map[string]error{
		"203.0.113.7:8090":                      firstErr,
		net.JoinHostPort("2001:db8::1", "8090"): errors.New("ipv6 refused"),
	}
	dial := yagoegress.PreferIPv4(
		staticResolver("203.0.113.7", "2001:db8::1"),
		recordingDial(&dialed, fail),
	)

	_, err := dial(context.Background(), "tcp", "peer.example:8090")
	if !errors.Is(err, firstErr) {
		t.Fatalf("err = %v, want the first (IPv4) dial error", err)
	}
}

func TestPreferIPv4PassesLiteralAddressStraightThrough(t *testing.T) {
	var dialed []string
	resolverCalls := 0
	resolve := func(context.Context, string) ([]netip.Addr, error) {
		resolverCalls++
		return nil, nil
	}
	dial := yagoegress.PreferIPv4(resolve, recordingDial(&dialed, nil))

	if _, err := dial(context.Background(), "tcp", "203.0.113.7:8090"); err != nil {
		t.Fatalf("dial: %v", err)
	}
	if resolverCalls != 0 || len(dialed) != 1 || dialed[0] != "203.0.113.7:8090" {
		t.Fatalf(
			"literal IP must dial unchanged without resolving; dialed=%v calls=%d",
			dialed,
			resolverCalls,
		)
	}
}

func TestPreferIPv4PassesUnparseableAddressStraightThrough(t *testing.T) {
	var dialed []string
	dial := yagoegress.PreferIPv4(staticResolver("203.0.113.7"), recordingDial(&dialed, nil))

	if _, err := dial(context.Background(), "tcp", "no-port-here"); err != nil {
		t.Fatalf("dial: %v", err)
	}
	if len(dialed) != 1 || dialed[0] != "no-port-here" {
		t.Fatalf("dialed = %v, want the raw address passed through", dialed)
	}
}

func TestPreferIPv4FallsBackToRawDialWhenResolutionFails(t *testing.T) {
	var dialed []string
	resolve := func(context.Context, string) ([]netip.Addr, error) {
		return nil, errors.New("nxdomain")
	}
	dial := yagoegress.PreferIPv4(resolve, recordingDial(&dialed, nil))

	if _, err := dial(context.Background(), "tcp", "peer.example:8090"); err != nil {
		t.Fatalf("dial: %v", err)
	}
	if len(dialed) != 1 || dialed[0] != "peer.example:8090" {
		t.Fatalf("dialed = %v, want the hostname dialed once as a fallback", dialed)
	}
}

func TestPreferIPv4NilResolverUsesSystemResolver(t *testing.T) {
	var dialed []string
	dial := yagoegress.PreferIPv4(nil, recordingDial(&dialed, nil))

	// localhost resolves via the system resolver to 127.0.0.1 (and ::1 where
	// present); IPv4-first must dial the IPv4 loopback.
	if _, err := dial(context.Background(), "tcp", "localhost:8090"); err != nil {
		t.Fatalf("dial: %v", err)
	}
	if len(dialed) == 0 || dialed[0] != "127.0.0.1:8090" {
		t.Fatalf("dialed = %v, want 127.0.0.1 first from the system resolver", dialed)
	}
}
