package contentcluster

import (
	"context"
	"fmt"
	"hash/fnv"
	"slices"
	"sync"
)

const evidenceBoundaryStripes = 4096

type evidenceBoundaries struct {
	stripes [evidenceBoundaryStripes]chan struct{}
}

type evidenceLease struct {
	boundaries *evidenceBoundaries
	indices    []int
	release    sync.Once
}

func newEvidenceBoundaries() *evidenceBoundaries {
	boundaries := &evidenceBoundaries{}
	for index := range boundaries.stripes {
		boundaries.stripes[index] = make(chan struct{}, 1)
		boundaries.stripes[index] <- struct{}{}
	}

	return boundaries
}

func (b *evidenceBoundaries) acquire(
	ctx context.Context,
	urls []string,
) (func(), error) {
	lease, err := b.acquireLease(ctx, urls)
	if err != nil {
		return nil, err
	}

	return lease.close, nil
}

func (b *evidenceBoundaries) acquireLease(
	ctx context.Context,
	identities []string,
) (*evidenceLease, error) {
	indices := evidenceBoundaryIndices(identities)
	acquired := make([]int, 0, len(indices))
	for _, index := range indices {
		select {
		case <-ctx.Done():
			b.release(acquired)

			return nil, fmt.Errorf("acquire evidence boundary: %w", ctx.Err())
		case <-b.stripes[index]:
			acquired = append(acquired, index)
		}
	}

	return &evidenceLease{boundaries: b, indices: acquired}, nil
}

func (l *evidenceLease) close() {
	if l == nil {
		return
	}
	l.release.Do(func() { l.boundaries.release(l.indices) })
}

func (l *evidenceLease) covers(identities []string) bool {
	covered := make(map[int]struct{}, len(l.indices))
	for _, index := range l.indices {
		covered[index] = struct{}{}
	}
	for _, index := range evidenceBoundaryIndices(identities) {
		if _, found := covered[index]; !found {
			return false
		}
	}

	return true
}

func (b *evidenceBoundaries) release(indices []int) {
	for position := len(indices) - 1; position >= 0; position-- {
		b.stripes[indices[position]] <- struct{}{}
	}
}

func evidenceBoundaryIndices(urls []string) []int {
	indices := make([]int, 0, len(urls))
	seen := make(map[int]struct{}, len(urls))
	for _, url := range urls {
		hasher := fnv.New32a()
		_, _ = hasher.Write([]byte(url))
		index := int(hasher.Sum32() % evidenceBoundaryStripes)
		if _, found := seen[index]; found {
			continue
		}
		seen[index] = struct{}{}
		indices = append(indices, index)
	}
	slices.Sort(indices)

	return indices
}
