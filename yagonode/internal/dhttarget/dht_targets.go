package dhttarget

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/D4rk4/yago/yagomodel"
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
	Peer     yagomodel.Seed
	Distance uint64
}

var errInvalidPosition = errors.New("invalid dht position")

func Select(
	start yagomodel.Hash,
	peers []yagomodel.Seed,
	config Config,
) ([]Target, error) {
	startPosition, err := yagomodel.Position(start)
	if err != nil {
		return nil, fmt.Errorf("dht start: %w", err)
	}

	return SelectAtPosition(startPosition, peers, config)
}

func SelectAtPosition(
	startPosition uint64,
	peers []yagomodel.Seed,
	config Config,
) ([]Target, error) {
	if startPosition > yagomodel.MaxPosition {
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

		position, err := yagomodel.Position(peer.Hash)
		if err != nil {
			continue
		}

		targets = append(targets, Target{
			Peer:     peer,
			Distance: yagomodel.Distance(startPosition, position),
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

func canReceiveIndex(peer yagomodel.Seed, config Config, now time.Time) bool {
	if _, ok := peer.NetworkAddress(); !ok {
		return false
	}
	if classification, known := peer.PeerType.Get(); known &&
		classification == yagomodel.PeerJunior {
		return false
	}

	flags, ok := peer.Flags.Get()
	if !ok || !flags.Get(yagomodel.FlagAcceptRemoteIndex) {
		return false
	}

	if config.MinimumAgeDays > 0 && peer.AgeDays(now) < config.MinimumAgeDays {
		return false
	}

	return hasEnoughRWI(peer, config.MinimumRWICount)
}

func hasEnoughRWI(peer yagomodel.Seed, minimum int) bool {
	if minimum <= 0 {
		return true
	}

	count, ok := peer.RWICount.Get()

	return ok && count >= minimum
}
