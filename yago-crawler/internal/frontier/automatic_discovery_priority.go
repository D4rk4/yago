package frontier

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/D4rk4/yago/yago-crawler/internal/crawladmission"
	"github.com/D4rk4/yago/yago-crawler/internal/crawljob"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

type readyCandidate struct {
	index int
	score float64
}

type readySelection struct {
	job       crawljob.CrawlJob
	index     int
	wait      time.Duration
	due       bool
	contended bool
}

type CrawlRunSeed struct {
	Requests      []yagocrawlcontract.CrawlRequest
	Provenance    []byte
	Priority      yagocrawlcontract.CrawlOrderPriority
	OrderIdentity []byte
	LeaseID       string
}

func WithAutomaticDiscoveryPriority(enabled bool) Option {
	return func(frontier *Frontier) {
		frontier.prioritizeAutomaticDiscovery = enabled
	}
}

func (f *Frontier) SetAutomaticDiscoveryPriority(enabled bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.prioritizeAutomaticDiscovery == enabled {
		return
	}
	f.prioritizeAutomaticDiscovery = enabled
	f.automaticDiscoveryBurst = 0
}

func (f *Frontier) SeedRun(
	ctx context.Context,
	requests []yagocrawlcontract.CrawlRequest,
	provenance []byte,
	profile crawladmission.AdmissionProfile,
	finish func(succeeded bool),
) SeededRun {
	return f.seedRun(
		ctx,
		CrawlRunSeed{
			Requests:   requests,
			Provenance: provenance,
			Priority:   yagocrawlcontract.CrawlOrderPriorityNormal,
		},
		profile,
		finish,
	)
}

func (f *Frontier) SeedRunWithPriority(
	ctx context.Context,
	seed CrawlRunSeed,
	profile crawladmission.AdmissionProfile,
	finish func(succeeded bool),
) SeededRun {
	return f.seedRun(ctx, seed, profile, finish)
}

func normalizeCrawlOrderPriority(
	priority yagocrawlcontract.CrawlOrderPriority,
) yagocrawlcontract.CrawlOrderPriority {
	if priority == yagocrawlcontract.CrawlOrderPriorityAutomaticDiscovery {
		return priority
	}

	return yagocrawlcontract.CrawlOrderPriorityNormal
}

func (f *Frontier) automaticDiscoveryRunLocked(runID uuid.UUID) bool {
	run := f.state.runs[runID]

	return run != nil && run.priority == yagocrawlcontract.CrawlOrderPriorityAutomaticDiscovery
}

func (f *Frontier) preferredReadyCandidate(
	current readyCandidate,
	index int,
	job crawljob.CrawlJob,
	score float64,
) readyCandidate {
	if f.preferReadyJobLocked(job, score, current.index, current.score) {
		return readyCandidate{index: index, score: score}
	}

	return current
}

func (f *Frontier) selectReadyCandidate(
	all readyCandidate,
	normal readyCandidate,
	automatic readyCandidate,
) (readyCandidate, bool) {
	if !f.prioritizeAutomaticDiscovery {
		return all, false
	}
	if automatic.index < 0 || normal.index < 0 {
		if automatic.index >= 0 {
			return automatic, false
		}

		return normal, false
	}
	if f.automaticDiscoveryBurst < yagocrawlcontract.AutomaticDiscoveryPriorityBurst {
		return automatic, true
	}

	return normal, true
}

func (f *Frontier) recordAutomaticDiscoveryDispatchLocked(
	job crawljob.CrawlJob,
	contended bool,
) {
	if !f.prioritizeAutomaticDiscovery || !contended {
		f.automaticDiscoveryBurst = 0

		return
	}
	if f.automaticDiscoveryRunLocked(job.RunID) {
		f.automaticDiscoveryBurst++

		return
	}
	f.automaticDiscoveryBurst = 0
}
