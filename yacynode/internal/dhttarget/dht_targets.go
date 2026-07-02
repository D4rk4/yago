package dhttarget

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/D4rk4/yago/yacymodel"
)

type Config struct {
	Redundancy          int
	CandidateRedundancy int
	MinimumAgeDays      int
	MinimumRWICount     int
	Now                 time.Time
	RandomTargetIndex   func(int) (int, error)
}

type Target struct {
	Peer     yacymodel.Seed
	Distance uint64
}

var errInvalidPosition = errors.New("invalid dht position")

func Select(
	start yacymodel.Hash,
	peers []yacymodel.Seed,
	config Config,
) ([]Target, error) {
	startPosition, err := yacymodel.Position(start)
	if err != nil {
		return nil, fmt.Errorf("dht start: %w", err)
	}

	return SelectAtPosition(startPosition, peers, config)
}

func SelectAtPosition(
	startPosition uint64,
	peers []yacymodel.Seed,
	config Config,
) ([]Target, error) {
	if startPosition > yacymodel.MaxPosition {
		return nil, fmt.Errorf("%w: %d", errInvalidPosition, startPosition)
	}
	if config.Redundancy <= 0 || len(peers) == 0 {
		return nil, nil
	}

	now := config.Now
	if now.IsZero() {
		now = time.Now()
	}

	candidateRedundancy := candidateRedundancy(config)
	targets := make([]Target, 0, min(candidateRedundancy, len(peers)))
	for _, peer := range peers {
		if !canReceiveIndex(peer, config, now) {
			continue
		}

		position, err := yacymodel.Position(peer.Hash)
		if err != nil {
			continue
		}

		targets = append(targets, Target{
			Peer:     peer,
			Distance: yacymodel.Distance(startPosition, position),
		})
	}

	slices.SortFunc(targets, func(a, b Target) int {
		if a.Distance != b.Distance {
			return cmp.Compare(a.Distance, b.Distance)
		}

		return cmp.Compare(a.Peer.Hash.String(), b.Peer.Hash.String())
	})
	if len(targets) > candidateRedundancy {
		targets = targets[:candidateRedundancy]
	}

	return chooseTargets(targets, config.Redundancy, config.RandomTargetIndex)
}

func canReceiveIndex(peer yacymodel.Seed, config Config, now time.Time) bool {
	if _, ok := peer.NetworkAddress(); !ok {
		return false
	}

	flags, ok := peer.Flags.Get()
	if !ok || !flags.Get(yacymodel.FlagAcceptRemoteIndex) {
		return false
	}

	if config.MinimumAgeDays > 0 && peer.AgeDays(now) < config.MinimumAgeDays {
		return false
	}

	return hasEnoughRWI(peer, config.MinimumRWICount)
}

func hasEnoughRWI(peer yacymodel.Seed, minimum int) bool {
	if minimum <= 0 {
		return true
	}

	count, ok := peer.RWICount.Get()

	return ok && count >= minimum
}
