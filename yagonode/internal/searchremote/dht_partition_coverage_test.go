package searchremote

import (
	"strconv"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
)

func TestDHTSearchPeerSelectionPreservesEveryVerticalPartition(t *testing.T) {
	const exponent = 4
	peers := make([]yagomodel.Seed, 0, 32)
	for partition := range 1 << exponent {
		for candidate := range 2 {
			peers = append(peers, searchSeed(
				t,
				partitionSearchHash(partition, candidate).String(),
			))
		}
	}

	selected, err := selectDHTSearchPeers(
		[]yagomodel.Hash{yagomodel.WordHash("partitioned")},
		peers,
		dhtSearchPeerConfig{
			maxPeers:           16,
			redundancy:         2,
			minimumPeerAgeDays: -1,
			minimumPeerRWIs:    -1,
			partitionExponent:  exponent,
		},
	)
	if err != nil {
		t.Fatalf("selectDHTSearchPeers: %v", err)
	}
	if len(selected) != 16 {
		t.Fatalf("selected peers = %d, want 16", len(selected))
	}

	covered := make(map[uint64]struct{}, 16)
	for _, peer := range selected {
		partition, partitionErr := yagomodel.VerticalPartition(peer.Hash, exponent)
		if partitionErr != nil {
			t.Fatalf("partition %q: %v", peer.Hash, partitionErr)
		}
		covered[partition] = struct{}{}
	}
	if len(covered) != 16 {
		t.Fatalf("covered partitions = %v, want all 16", covered)
	}
}

func TestPartitionAwareSearchPeersFillsRedundancyAfterCoverage(t *testing.T) {
	first := searchSeed(t, partitionSearchHash(0, 0).String())
	second := searchSeed(t, partitionSearchHash(1, 0).String())
	third := searchSeed(t, partitionSearchHash(0, 1).String())
	selected := partitionAwareSearchPeers([][][]yagomodel.Seed{{
		{first, third},
		{second},
	}}, 3)
	if len(selected) != 3 ||
		selected[0].Hash != first.Hash ||
		selected[1].Hash != second.Hash ||
		selected[2].Hash != third.Hash {
		t.Fatalf("selected peers = %#v", selected)
	}
	if selected := partitionAwareSearchPeers(nil, 16); selected != nil {
		t.Fatalf("empty targets = %#v", selected)
	}
	if selected := partitionAwareSearchPeers([][][]yagomodel.Seed{{{first}}}, 0); selected != nil {
		t.Fatalf("zero maximum targets = %#v", selected)
	}
	if selected := partitionAwareSearchPeers([][][]yagomodel.Seed{{
		{first},
		{second},
	}}, 1); len(selected) != 1 || selected[0].Hash != first.Hash {
		t.Fatalf("coverage cap targets = %#v", selected)
	}
	if !partitionHasSelectedPeer(
		[]yagomodel.Seed{first},
		map[yagomodel.Hash]struct{}{first.Hash: {}},
	) {
		t.Fatal("selected peer did not cover its target partition")
	}
}

func partitionSearchHash(partition int, candidate int) yagomodel.Hash {
	return yagomodel.Hash(
		string(yagomodel.Alphabet[partition<<2]) +
			strconv.Itoa(candidate) +
			"AAAAAAAAAA",
	)
}
