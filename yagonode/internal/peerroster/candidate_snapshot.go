package peerroster

import (
	"container/heap"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const (
	candidateSnapshotMaximumPeers      = 4096
	candidateSnapshotMaximumBytes      = 16 << 20
	peerCandidateSnapshotFailedMessage = "peer candidate snapshot failed"
	rosterCandidateRetentionBytes      = 32
)

type rosterCandidate struct {
	entry         rosterEntry
	retainedBytes int
}

type freshestRosterCandidates []rosterCandidate

type candidateSnapshotAttempt struct {
	seeds    []yagomodel.Seed
	building chan struct{}
	revision uint64
	ready    bool
	builder  bool
}

func (c freshestRosterCandidates) Len() int {
	return len(c)
}

func (c freshestRosterCandidates) Less(left, right int) bool {
	comparison := c[left].entry.lastSeen.Compare(c[right].entry.lastSeen)
	if comparison != 0 {
		return comparison < 0
	}

	return c[left].entry.seed.Hash.String() > c[right].entry.seed.Hash.String()
}

func (c freshestRosterCandidates) Swap(left, right int) {
	c[left], c[right] = c[right], c[left]
}

func (c *freshestRosterCandidates) Push(value any) {
	*c = append(*c, value.(rosterCandidate))
}

func (c *freshestRosterCandidates) Pop() any {
	values := *c
	last := len(values) - 1
	value := values[last]
	values[last] = rosterCandidate{}
	*c = values[:last]

	return value
}

func (r *roster) invalidateCandidateSnapshot() {
	r.candidateMu.Lock()
	r.candidateRevision++
	r.candidateReady = false
	r.candidateSeeds = nil
	r.candidateBytes = 0
	r.candidateMu.Unlock()
}

func (r *roster) freshestCandidateSnapshot(
	ctx context.Context,
	limit int,
) []yagomodel.Seed {
	if limit <= 0 {
		return nil
	}
	limit = min(limit, candidateSnapshotMaximumPeers)

	for ctx.Err() == nil {
		attempt := r.beginCandidateSnapshot()
		if attempt.ready {
			return detachCandidateSeeds(attempt.seeds, limit)
		}
		if !attempt.builder {
			if waitForCandidateSnapshot(ctx, attempt.building) {
				continue
			}

			return nil
		}
		seeds, retainedBytes, err := r.buildCandidateSnapshot(ctx)
		stable := r.finishCandidateSnapshot(attempt, seeds, retainedBytes, err)
		if candidateSnapshotBuildFailed(ctx, err) {
			return nil
		}
		if stable {
			return detachCandidateSeeds(seeds, limit)
		}
	}

	return nil
}

func (r *roster) beginCandidateSnapshot() candidateSnapshotAttempt {
	r.candidateMu.Lock()
	defer r.candidateMu.Unlock()

	if r.candidateReady {
		return candidateSnapshotAttempt{seeds: r.candidateSeeds, ready: true}
	}
	if r.candidateBuilding != nil {
		return candidateSnapshotAttempt{building: r.candidateBuilding}
	}

	building := make(chan struct{})
	r.candidateBuilding = building

	return candidateSnapshotAttempt{
		building: building,
		revision: r.candidateRevision,
		builder:  true,
	}
}

func waitForCandidateSnapshot(ctx context.Context, building <-chan struct{}) bool {
	select {
	case <-ctx.Done():
		return false
	case <-building:
		return true
	}
}

func (r *roster) finishCandidateSnapshot(
	attempt candidateSnapshotAttempt,
	seeds []yagomodel.Seed,
	retainedBytes int,
	err error,
) bool {
	r.candidateMu.Lock()
	defer r.candidateMu.Unlock()

	stable := attempt.revision == r.candidateRevision
	if err == nil && stable {
		r.candidateSeeds = seeds
		r.candidateBytes = retainedBytes
		r.candidateReady = true
	}
	if r.candidateBuilding == attempt.building {
		r.candidateBuilding = nil
	}
	close(attempt.building)

	return stable
}

func candidateSnapshotBuildFailed(ctx context.Context, err error) bool {
	if err == nil {
		return false
	}
	if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		slog.WarnContext(ctx, peerCandidateSnapshotFailedMessage, slog.Any("error", err))
	}

	return true
}

func (r *roster) buildCandidateSnapshot(
	ctx context.Context,
) ([]yagomodel.Seed, int, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, fmt.Errorf("build peer candidate snapshot: %w", err)
	}
	peerLimit := min(max(r.reservoirCap, 0), candidateSnapshotMaximumPeers)
	byteLimit := min(max(r.candidateByteLimit, 0), candidateSnapshotMaximumBytes)
	if peerLimit == 0 || byteLimit == 0 {
		return []yagomodel.Seed{}, 0, nil
	}

	activeSeeds, activeHashes := r.activeSnapshot()
	seeds := make([]yagomodel.Seed, 0, min(peerLimit, len(activeSeeds)))
	retainedBytes := 0
	for _, seed := range activeSeeds {
		if len(seeds) == peerLimit {
			break
		}
		owned := seed.Copy()
		ownedBytes := owned.RetainedBytes()
		if ownedBytes > byteLimit-retainedBytes {
			continue
		}
		seeds = append(seeds, owned)
		retainedBytes += ownedBytes
	}

	inactive, inactiveBytes, err := r.scanFreshestCandidates(
		ctx,
		activeHashes,
		peerLimit-len(seeds),
		byteLimit-retainedBytes,
	)
	if err != nil {
		return nil, 0, err
	}
	for _, candidate := range inactive {
		seeds = append(seeds, candidate.entry.seed)
	}

	return seeds, retainedBytes + inactiveBytes, nil
}

func (r *roster) scanFreshestCandidates(
	ctx context.Context,
	active map[yagomodel.Hash]struct{},
	peerLimit int,
	byteLimit int,
) ([]rosterCandidate, int, error) {
	if peerLimit <= 0 || byteLimit <= 0 {
		return nil, 0, nil
	}

	var candidates freshestRosterCandidates
	retainedBytes := 0
	if err := r.vault.View(ctx, func(tx *vault.Txn) error {
		return r.peers.Scan(tx, nil, func(_ vault.Key, entry rosterEntry) (bool, error) {
			if err := ctx.Err(); err != nil {
				return false, fmt.Errorf("scan peer candidate context: %w", err)
			}
			if _, ok := active[entry.seed.Hash]; ok {
				return true, nil
			}

			entryBytes := entry.seed.RetainedBytes() + rosterCandidateRetentionBytes
			if entryBytes > byteLimit {
				return true, nil
			}
			heap.Push(&candidates, rosterCandidate{entry: entry, retainedBytes: entryBytes})
			retainedBytes += entryBytes
			for len(candidates) > peerLimit || retainedBytes > byteLimit {
				removed := heap.Pop(&candidates).(rosterCandidate)
				retainedBytes -= removed.retainedBytes
			}

			return true, nil
		})
	}); err != nil {
		return nil, 0, fmt.Errorf("scan peer candidates: %w", err)
	}

	sort.Slice(candidates, func(left, right int) bool {
		comparison := candidates[left].entry.lastSeen.Compare(candidates[right].entry.lastSeen)
		if comparison != 0 {
			return comparison > 0
		}

		return candidates[left].entry.seed.Hash.String() < candidates[right].entry.seed.Hash.String()
	})

	return []rosterCandidate(candidates), retainedBytes, nil
}

func detachCandidateSeeds(seeds []yagomodel.Seed, limit int) []yagomodel.Seed {
	limit = min(max(limit, 0), len(seeds))
	detached := make([]yagomodel.Seed, 0, limit)
	for _, seed := range seeds {
		if len(detached) == limit {
			break
		}
		detached = append(detached, detachCandidateSeed(seed))
	}

	return detached
}

func detachCandidateSeed(seed yagomodel.Seed) yagomodel.Seed {
	detached := seed
	if hosts, ok := seed.IP6.Get(); ok {
		detached.IP6 = yagomodel.Some(append([]yagomodel.Host(nil), hosts...))
	}

	return detached
}
