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
	pinger  ports.PeerPinger
	maxSize int
	order   []yacymodel.Hash
	seeds   map[yacymodel.Hash]yacymodel.Seed
}

func NewPeerDirectory(clock ports.Clock, pinger ports.PeerPinger, maxSize int) *PeerDirectory {
	return &PeerDirectory{
		clock:   clock,
		pinger:  pinger,
		maxSize: maxSize,
		seeds:   make(map[yacymodel.Hash]yacymodel.Seed),
	}
}

func (d *PeerDirectory) Hello(
	ctx context.Context,
	caller yacymodel.Seed,
) (contracts.HelloOutcome, error) {
	callerType := d.classifyCaller(ctx, caller)
	d.record(caller)

	return contracts.HelloOutcome{
		CallerType: callerType,
		Known:      d.snapshot(),
	}, nil
}

func (d *PeerDirectory) classifyCaller(
	ctx context.Context,
	caller yacymodel.Seed,
) yacymodel.PeerType {
	if !advertisesReachableEndpoint(caller) {
		return yacymodel.PeerJunior
	}
	if err := d.pinger.Ping(ctx, caller); err != nil {
		return yacymodel.PeerJunior
	}

	return yacymodel.PeerSenior
}

func advertisesReachableEndpoint(caller yacymodel.Seed) bool {
	if caller[yacymodel.SeedIP] == "" {
		return false
	}
	port, err := caller.Port()

	return err == nil && port > 0
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
