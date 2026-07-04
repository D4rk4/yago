package searchremote

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"slices"

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
	selected := make([]yagomodel.Seed, 0, config.maxPeers)
	seen := make(map[yagomodel.Hash]struct{})
	partitions := 1 << config.partitionExponent
	for _, hash := range hashes {
		for partition := range partitions {
			position, err := yagomodel.VerticalPosition(
				hash,
				uint64(partition),
				config.partitionExponent,
			)
			if err != nil {
				continue
			}
			if err := appendDHTSearchPeers(&selected, seen, position, peers, config); err != nil {
				return nil, err
			}
		}
	}

	return limitDHTSearchPeers(selected, config)
}

func appendDHTSearchPeers(
	selected *[]yagomodel.Seed,
	seen map[yagomodel.Hash]struct{},
	position uint64,
	peers []yagomodel.Seed,
	config dhtSearchPeerConfig,
) error {
	targets, err := dhttarget.SelectAtPosition(position, peers, dhttarget.Config{
		Redundancy:          config.redundancy,
		CandidateRedundancy: candidateRedundancy(config),
		MinimumAgeDays:      config.minimumPeerAgeDays,
		MinimumRWICount:     config.minimumPeerRWIs,
		RandomTargetIndex:   config.randomTargetIndex,
	})
	if err != nil {
		return fmt.Errorf("select dht search target: %w", err)
	}
	for _, target := range targets {
		if _, ok := seen[target.Peer.Hash]; ok {
			continue
		}
		seen[target.Peer.Hash] = struct{}{}
		*selected = append(*selected, target.Peer)
	}

	return nil
}

func limitDHTSearchPeers(
	peers []yagomodel.Seed,
	config dhtSearchPeerConfig,
) ([]yagomodel.Seed, error) {
	if config.maxPeers <= 0 || len(peers) <= config.maxPeers {
		return peers, nil
	}

	candidates := slices.Clone(peers)
	selected := make([]yagomodel.Seed, 0, config.maxPeers)
	for range config.maxPeers {
		index, err := chooseSearchTargetIndex(config.randomTargetIndex, len(candidates))
		if err != nil {
			return nil, err
		}
		selected = append(selected, candidates[index])
		candidates = slices.Delete(candidates, index, index+1)
	}

	return selected, nil
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

func chooseSearchTargetIndex(
	randomTargetIndex func(int) (int, error),
	upper int,
) (int, error) {
	index, err := randomTargetIndexOrDefault(randomTargetIndex)(upper)
	if err != nil {
		return 0, fmt.Errorf("choose dht search target: %w", err)
	}
	if index < 0 || index >= upper {
		return 0, fmt.Errorf(
			"choose dht search target: index %d outside %d candidates",
			index,
			upper,
		)
	}

	return index, nil
}

func secureRandomTargetIndex(upper int) (int, error) {
	value, err := randomInteger(rand.Reader, big.NewInt(int64(upper)))
	if err != nil {
		return 0, fmt.Errorf("random dht search target: %w", err)
	}

	return int(value.Int64()), nil
}
