package yagonode

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
)

type scriptedSelfSeed struct {
	seed yagomodel.Seed
}

func (s scriptedSelfSeed) SelfSeed(context.Context) yagomodel.Seed { return s.seed }

func TestNetworkSourceReportsAdvertisedFlags(t *testing.T) {
	flags := yagomodel.ZeroFlags().
		Set(yagomodel.FlagDirectConnect, true).
		Set(yagomodel.FlagAcceptRemoteIndex, true)
	source := newNetworkSource(dhtGateStatusSource{}, nil, nil, nil, nil).
		withSelf(scriptedSelfSeed{seed: yagomodel.Seed{Flags: yagomodel.Some(flags)}})

	status := source.Network(context.Background())

	if len(status.OwnFlags) != len(seedFlagBits) {
		t.Fatalf("own flags = %d entries, want %d: %+v",
			len(status.OwnFlags), len(seedFlagBits), status.OwnFlags)
	}
	states := map[string]bool{}
	for _, flag := range status.OwnFlags {
		states[flag.Name] = flag.Set
	}
	if !states["direct"] || !states["remote-index"] {
		t.Fatalf("advertised bits missing: %+v", states)
	}
	if states["remote-crawl"] || states["root"] || states["ssl"] {
		t.Fatalf("unset bits reported as advertised: %+v", states)
	}
}

func TestNetworkSourceHidesFlagsWithoutProviderOrFlags(t *testing.T) {
	source := newNetworkSource(dhtGateStatusSource{}, nil, nil, nil, nil)
	if got := source.Network(context.Background()).OwnFlags; got != nil {
		t.Fatalf("own flags without a provider = %+v, want nil", got)
	}

	bare := source.withSelf(scriptedSelfSeed{seed: yagomodel.Seed{}})
	if got := bare.Network(context.Background()).OwnFlags; got != nil {
		t.Fatalf("own flags without seed flags = %+v, want nil", got)
	}
}
