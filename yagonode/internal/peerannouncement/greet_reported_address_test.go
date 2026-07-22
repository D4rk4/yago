package peerannouncement

import (
	"testing"

	"github.com/D4rk4/yago/yagomodel"
)

func TestGreetReportedAddressAcceptsUpstreamAddressSemantics(t *testing.T) {
	hostnameSelf := callerSeed(t, "self", "yagoseek.dev")
	ipSelf := callerSeed(t, "self", "203.0.113.9")

	for _, test := range []struct {
		name     string
		value    string
		selfSeed yagomodel.Seed
		want     bool
	}{
		{name: "advertised hostname", value: "yagoseek.dev", selfSeed: hostnameSelf, want: true},
		{name: "canonical advertised hostname", value: "YAGOSEEK.DEV.", selfSeed: hostnameSelf, want: true},
		{name: "ipv4 literal", value: "203.0.113.9", selfSeed: hostnameSelf, want: true},
		{name: "ipv6 literal", value: "2001:db8::9", selfSeed: hostnameSelf, want: true},
		{name: "mixed invalid and ipv4", value: "not a host, 203.0.113.9", selfSeed: hostnameSelf, want: true},
		{name: "mixed invalid and advertised hostname", value: "other.example, yagoseek.dev", selfSeed: hostnameSelf, want: true},
		{name: "empty", selfSeed: hostnameSelf, want: false},
		{name: "unspecified ipv4", value: "0.0.0.0", selfSeed: hostnameSelf, want: false},
		{name: "unspecified ipv6", value: "::", selfSeed: hostnameSelf, want: false},
		{name: "mapped unspecified ipv4", value: "::ffff:0.0.0.0", selfSeed: hostnameSelf, want: false},
		{name: "mismatched hostname", value: "other.example", selfSeed: hostnameSelf, want: false},
		{name: "hostname without hostname seed", value: "yagoseek.dev", selfSeed: ipSelf, want: false},
		{name: "hostname without advertised address", value: "yagoseek.dev", selfSeed: yagomodel.Seed{}, want: false},
		{name: "invalid list", value: "other.example, not a host", selfSeed: hostnameSelf, want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := validGreetReportedAddresses(test.value, test.selfSeed); got != test.want {
				t.Fatalf(
					"validGreetReportedAddresses(%q) = %t, want %t",
					test.value,
					got,
					test.want,
				)
			}
		})
	}
}
