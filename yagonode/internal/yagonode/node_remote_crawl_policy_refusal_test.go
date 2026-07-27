package yagonode

import (
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/remotecrawl"
	"github.com/D4rk4/yago/yagoproto"
)

// completeRemoteCrawlPolicy is the only shape remote crawl may run in: enabled,
// on a salted-magic private network with a shared secret, with at least one
// trusted peer and one allowed destination.
func completeRemoteCrawlPolicy() nodeConfig {
	return nodeConfig{
		NetworkAuthenticationMode:   yagoproto.NetworkAuthenticationSaltedMagic,
		NetworkAuthenticationSecret: "shared",
		RemoteCrawl: remoteCrawlConfig{
			Enabled:             true,
			TrustedPeers:        []yagomodel.Hash{yagomodel.Hash("AAAAAAAAAAAA")},
			AllowedDestinations: []string{"example.com"},
			RequestsPerMinute:   remotecrawl.DefaultRequestsPerMinute,
			OutstandingPerPeer:  remotecrawl.DefaultOutstandingPerPeer,
			LeaseTTL:            remotecrawl.DefaultLeaseTTL,
			QueueCapacity:       remotecrawl.DefaultQueueCapacity,
		},
	}
}

// Every remote crawl requirement is checked on one call path, so a test that
// only asks whether some error came back cannot tell the requirements apart:
// deleting the trusted-peer requirement leaves the destination requirement to
// refuse the very same incomplete policy, and nothing notices that remote crawl
// would now accept work from any peer on the network. Each requirement is
// therefore removed on its own from an otherwise complete policy, and the
// refusal is read back by name.
func TestRemoteCrawlPolicyNamesTheMissingRequirement(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name    string
		strip   func(nodeConfig) nodeConfig
		refusal string
	}{
		{
			name: "uncontrolled network",
			strip: func(config nodeConfig) nodeConfig {
				config.NetworkAuthenticationMode = yagoproto.NetworkAuthenticationUncontrolled

				return config
			},
			refusal: "salted-magic network authentication",
		},
		{
			name: "missing shared secret",
			strip: func(config nodeConfig) nodeConfig {
				config.NetworkAuthenticationSecret = ""

				return config
			},
			refusal: "salted-magic network authentication",
		},
		{
			name: "no trusted peer",
			strip: func(config nodeConfig) nodeConfig {
				config.RemoteCrawl.TrustedPeers = nil

				return config
			},
			refusal: "at least one trusted peer hash",
		},
		{
			name: "no allowed destination",
			strip: func(config nodeConfig) nodeConfig {
				config.RemoteCrawl.AllowedDestinations = nil

				return config
			},
			refusal: "at least one allowed destination",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := validateRemoteCrawlConfig(test.strip(completeRemoteCrawlPolicy()))
			if err == nil {
				t.Fatal("incomplete remote crawl policy accepted")
			}
			if !strings.Contains(err.Error(), test.refusal) {
				t.Fatalf("refusal = %q, want it to name %q", err, test.refusal)
			}
		})
	}
}

// The complete policy is the accepting side of the same gate, and a disabled
// policy is admitted whatever else is missing: default-deny remote crawl must
// not turn an operator's untouched node into a startup failure.
func TestRemoteCrawlPolicyAdmitsCompleteAndDisabledPolicies(t *testing.T) {
	t.Parallel()

	if err := validateRemoteCrawlConfig(completeRemoteCrawlPolicy()); err != nil {
		t.Fatalf("complete remote crawl policy refused: %v", err)
	}
	disabled := completeRemoteCrawlPolicy()
	disabled.RemoteCrawl = remoteCrawlConfig{}
	disabled.NetworkAuthenticationMode = yagoproto.NetworkAuthenticationUncontrolled
	disabled.NetworkAuthenticationSecret = ""
	if err := validateRemoteCrawlConfig(disabled); err != nil {
		t.Fatalf("disabled remote crawl policy refused: %v", err)
	}
}
