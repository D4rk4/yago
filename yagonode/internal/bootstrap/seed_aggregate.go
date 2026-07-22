package bootstrap

import (
	"container/heap"
	"slices"
	"time"

	"github.com/D4rk4/yago/yagomodel"
)

type aggregatedSeed struct {
	seed          yagomodel.Seed
	source        int
	position      int
	retainedBytes int
	heapPosition  int
}

type seedAggregate struct {
	seeds         []*aggregatedSeed
	byHash        map[yagomodel.Hash]*aggregatedSeed
	retainedBytes int
}

func newSeedAggregate() *seedAggregate {
	return &seedAggregate{byHash: make(map[yagomodel.Hash]*aggregatedSeed)}
}

func (a seedAggregate) Len() int {
	return len(a.seeds)
}

func (a seedAggregate) Less(left, right int) bool {
	return compareAdvertisedFreshness(a.seeds[left].seed, a.seeds[right].seed) > 0
}

func (a seedAggregate) Swap(left, right int) {
	a.seeds[left], a.seeds[right] = a.seeds[right], a.seeds[left]
	a.seeds[left].heapPosition = left
	a.seeds[right].heapPosition = right
}

func (a *seedAggregate) Push(value any) {
	seed := value.(*aggregatedSeed)
	seed.heapPosition = len(a.seeds)
	a.seeds = append(a.seeds, seed)
}

func (a *seedAggregate) Pop() any {
	last := len(a.seeds) - 1
	seed := a.seeds[last]
	a.seeds[last] = nil
	a.seeds = a.seeds[:last]
	seed.heapPosition = -1

	return seed
}

func (a *seedAggregate) admit(
	seed yagomodel.Seed,
	source int,
	position int,
	now time.Time,
) {
	if !seedFreshEnough(seed, now) {
		return
	}
	retainedBytes := seed.RetainedBytes()
	if retainedBytes > seedlistMaxRetainedBytes {
		return
	}
	if stored, found := a.byHash[seed.Hash]; found {
		if !aggregateSeedPreferred(seed, source, position, stored) {
			return
		}
		a.retainedBytes += retainedBytes - stored.retainedBytes
		stored.seed = seed.Copy()
		stored.source = source
		stored.position = position
		stored.retainedBytes = retainedBytes
		heap.Fix(a, stored.heapPosition)
	} else {
		stored := &aggregatedSeed{
			seed:          seed.Copy(),
			source:        source,
			position:      position,
			retainedBytes: retainedBytes,
		}
		a.byHash[seed.Hash] = stored
		a.retainedBytes += retainedBytes
		heap.Push(a, stored)
	}
	for a.Len() > seedlistMaxEntries || a.retainedBytes > seedlistMaxRetainedBytes {
		discarded := heap.Pop(a).(*aggregatedSeed)
		delete(a.byHash, discarded.seed.Hash)
		a.retainedBytes -= discarded.retainedBytes
	}
}

func aggregateSeedPreferred(
	seed yagomodel.Seed,
	source int,
	position int,
	stored *aggregatedSeed,
) bool {
	if advertisedAfter(seed, stored.seed) {
		return true
	}
	if advertisedAfter(stored.seed, seed) {
		return false
	}
	if source != stored.source {
		return source < stored.source
	}

	return position < stored.position
}

func (a *seedAggregate) result() []yagomodel.Seed {
	seeds := make([]yagomodel.Seed, 0, len(a.seeds))
	for _, retained := range a.seeds {
		seeds = append(seeds, retained.seed)
	}
	slices.SortFunc(seeds, compareAdvertisedFreshness)

	return seeds
}
