package services

import (
	"context"
	"maps"

	"github.com/nikitakarpei/yacy-rwi-node/internal/core/contracts"
	"github.com/nikitakarpei/yacy-rwi-node/internal/core/ports"
	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

type PeerDirectory struct {
	clock   ports.Clock
	maxSize int
	order   []yacymodel.Hash
	seeds   map[yacymodel.Hash]yacymodel.Seed
}

func NewPeerDirectory(clock ports.Clock, maxSize int) *PeerDirectory {
	return &PeerDirectory{
		clock:   clock,
		maxSize: maxSize,
		seeds:   make(map[yacymodel.Hash]yacymodel.Seed),
	}
}

func (d *PeerDirectory) Hello(
	_ context.Context,
	caller yacymodel.Seed,
) (contracts.HelloOutcome, error) {
	callerType := classifyCaller(caller)
	d.record(caller)

	return contracts.HelloOutcome{
		CallerType: callerType,
		Known:      d.snapshot(),
	}, nil
}

func classifyCaller(caller yacymodel.Seed) yacymodel.PeerType {
	if caller[yacymodel.SeedIP] == "" {
		return yacymodel.PeerJunior
	}
	if port, err := caller.Port(); err != nil || port <= 0 {
		return yacymodel.PeerJunior
	}

	return yacymodel.PeerSenior
}

func (d *PeerDirectory) record(caller yacymodel.Seed) {
	hash, err := caller.Hash()
	if err != nil {
		return
	}

	stamped := make(yacymodel.Seed, len(caller)+1)
	maps.Copy(stamped, caller)
	stamped[yacymodel.SeedLastSeen] = d.clock.Now().UTC().Format(seedUTCLayout)

	if _, exists := d.seeds[hash]; !exists {
		if len(d.order) >= d.maxSize {
			oldest := d.order[0]
			d.order = d.order[1:]
			delete(d.seeds, oldest)
		}
		d.order = append(d.order, hash)
	}
	d.seeds[hash] = stamped
}

func (d *PeerDirectory) snapshot() []yacymodel.Seed {
	known := make([]yacymodel.Seed, 0, len(d.order))
	for _, hash := range d.order {
		known = append(known, d.seeds[hash])
	}

	return known
}
