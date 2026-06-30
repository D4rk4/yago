// Package peerroster owns the set of network peers this node knows. It is the
// single owner of each peer's recency and reachable membership: callers report
// discoveries through Discover, reachability observations through Reachable and
// Unreachable, and read the reachable peers through ReachablePeers. Only the
// bounded reachable set lives in memory; every known peer is persisted, so a
// restart resumes from the durable roster instead of the seed source.
package peerroster

import (
	"fmt"
	"sync"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/vault"
)

const peersBucket vault.Name = "peerroster"

type Roster struct {
	vault        *vault.Vault
	peers        *vault.Collection[rosterEntry]
	now          func() time.Time
	reservoirCap int
	activeCap    int

	mu     sync.Mutex
	active map[yacymodel.Hash]yacymodel.Seed
}

func Open(
	storage *vault.Vault,
	now func() time.Time,
	reservoirCap int,
	activeCap int,
) (*Roster, error) {
	peers, err := vault.Register(storage, peersBucket, rosterEntryCodec{})
	if err != nil {
		return nil, fmt.Errorf("register peer roster: %w", err)
	}

	return &Roster{
		vault:        storage,
		peers:        peers,
		now:          now,
		reservoirCap: reservoirCap,
		activeCap:    activeCap,
		active:       make(map[yacymodel.Hash]yacymodel.Seed),
	}, nil
}

func (r *Roster) key(hash yacymodel.Hash) vault.Key {
	return vault.Key(hash.String())
}
