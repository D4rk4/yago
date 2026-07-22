package bootstrap

import (
	"cmp"
	"context"
	"time"

	"github.com/D4rk4/yago/yagomodel"
)

const (
	seedlistAggregateTimeout        = 10 * time.Second
	seedlistFetchConcurrency        = 8
	seedMaximumAge                  = 24 * time.Hour
	seedlistFetchFailedMessage      = "seedlist fetch failed"
	seedlistAggregateExpiredMessage = "seedlist aggregate deadline elapsed"
)

type seedlists struct {
	fetcher     seedlistFetcher
	urls        []string
	observer    SeedImportObserver
	now         func() time.Time
	timeout     time.Duration
	concurrency int
}

type seedlistFetcher interface {
	Fetch(context.Context, string) ([]yagomodel.Seed, error)
}

var _ SeedSource = (*seedlists)(nil)

func (s *seedlists) Fetch(ctx context.Context) []yagomodel.Seed {
	if len(s.urls) == 0 {
		return nil
	}
	refresh := s.startRefresh(ctx)
	defer refresh.cancel()

	return refresh.collect()
}

func seedFreshEnough(seed yagomodel.Seed, now time.Time) bool {
	seen, known := seed.LastSeen.Get()
	if !known {
		return true
	}
	instant := seen.Time()

	return !instant.After(now) && now.Sub(instant) <= seedMaximumAge
}

func advertisedAfter(left, right yagomodel.Seed) bool {
	leftSeen, leftKnown := left.LastSeen.Get()
	rightSeen, rightKnown := right.LastSeen.Get()
	if leftKnown != rightKnown {
		return leftKnown
	}

	return leftKnown && leftSeen.Time().After(rightSeen.Time())
}

func compareAdvertisedFreshness(left, right yagomodel.Seed) int {
	if advertisedAfter(left, right) {
		return -1
	}
	if advertisedAfter(right, left) {
		return 1
	}
	return cmp.Compare(left.Hash.String(), right.Hash.String())
}
