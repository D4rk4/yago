package indextransfer

import (
	"fmt"

	"github.com/D4rk4/yago/yacymodel"
	"github.com/D4rk4/yago/yacynode/internal/dhttarget"
)

type DHTTargetConfig = dhttarget.Config

type DHTTarget = dhttarget.Target

func SelectDHTTargets(
	start yacymodel.Hash,
	peers []yacymodel.Seed,
	config DHTTargetConfig,
) ([]DHTTarget, error) {
	targets, err := dhttarget.Select(start, peers, config)
	if err != nil {
		return nil, fmt.Errorf("select dht targets: %w", err)
	}

	return targets, nil
}

func SelectDHTTargetsAtPosition(
	startPosition uint64,
	peers []yacymodel.Seed,
	config DHTTargetConfig,
) ([]DHTTarget, error) {
	targets, err := dhttarget.SelectAtPosition(startPosition, peers, config)
	if err != nil {
		return nil, fmt.Errorf("select dht targets at position: %w", err)
	}

	return targets, nil
}
