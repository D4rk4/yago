package searchremote

import (
	"crypto/rand"
	"fmt"
	"math/big"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/dhttarget"
)

var randomInteger = rand.Int

type dhtSearchPeerConfig struct {
	maxPeers           int
	redundancy         int
	minimumPeerAgeDays int
	minimumPeerRWIs    int
	partitionExponent  int
	randomTargetIndex  func(int) (int, error)
}

func selectDHTSearchPeers(
	hashes []yagomodel.Hash,
	peers []yagomodel.Seed,
	config dhtSearchPeerConfig,
) ([]yagomodel.Seed, error) {
	partitions := 1 << config.partitionExponent
	targets := make([][][]yagomodel.Seed, len(hashes))
	for termPosition, hash := range hashes {
		targets[termPosition] = make([][]yagomodel.Seed, partitions)
		for partition := range partitions {
			position, err := yagomodel.VerticalPosition(
				hash,
				uint64(partition),
				config.partitionExponent,
			)
			if err != nil {
				continue
			}
			selected, err := dhtSearchTargetsAtPosition(position, peers, config)
			if err != nil {
				return nil, err
			}
			targets[termPosition][partition] = selected
		}
	}

	return partitionAwareSearchPeers(targets, config.maxPeers), nil
}

func dhtSearchTargetsAtPosition(
	position uint64,
	peers []yagomodel.Seed,
	config dhtSearchPeerConfig,
) ([]yagomodel.Seed, error) {
	targets, err := dhttarget.SelectAtPosition(position, peers, dhttarget.Config{
		Redundancy:          config.redundancy,
		CandidateRedundancy: candidateRedundancy(config),
		MinimumAgeDays:      config.minimumPeerAgeDays,
		MinimumRWICount:     config.minimumPeerRWIs,
		RandomTargetIndex:   config.randomTargetIndex,
	})
	if err != nil {
		return nil, fmt.Errorf("select dht search target: %w", err)
	}
	selected := make([]yagomodel.Seed, len(targets))
	for position, target := range targets {
		selected[position] = target.Peer
	}

	return selected, nil
}

func partitionAwareSearchPeers(targets [][][]yagomodel.Seed, maximum int) []yagomodel.Seed {
	selection := newPartitionPeerSelection(targets, maximum)
	if selection == nil {
		return nil
	}
	if selection.coverPartitions() {
		selection.appendRemainingCandidates()
	}

	return selection.selected
}

func partitionHasSelectedPeer(
	peers []yagomodel.Seed,
	selected map[yagomodel.Hash]struct{},
) bool {
	for _, peer := range peers {
		if _, found := selected[peer.Hash]; found {
			return true
		}
	}

	return false
}

func candidateRedundancy(config dhtSearchPeerConfig) int {
	if config.maxPeers > config.redundancy {
		return config.maxPeers
	}

	return config.redundancy
}

func randomTargetIndexOrDefault(
	randomTargetIndex func(int) (int, error),
) func(int) (int, error) {
	if randomTargetIndex != nil {
		return randomTargetIndex
	}

	return secureRandomTargetIndex
}

func secureRandomTargetIndex(upper int) (int, error) {
	value, err := randomInteger(rand.Reader, big.NewInt(int64(upper)))
	if err != nil {
		return 0, fmt.Errorf("random dht search target: %w", err)
	}

	return int(value.Int64()), nil
}
