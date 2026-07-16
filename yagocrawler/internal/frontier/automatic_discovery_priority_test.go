package frontier

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawler/internal/crawladmission"
	"github.com/D4rk4/yago/yagocrawler/internal/crawljob"
)

type priorityPageSeed struct {
	provenance string
	priority   yagocrawlcontract.CrawlOrderPriority
	pages      int
}

func normalPageSeed(pages int) priorityPageSeed {
	return priorityPageSeed{provenance: "normal", pages: pages}
}

func automaticPageSeed(provenance string, pages int) priorityPageSeed {
	return priorityPageSeed{
		provenance: provenance,
		priority:   yagocrawlcontract.CrawlOrderPriorityAutomaticDiscovery,
		pages:      pages,
	}
}

func seedPriorityPages(
	t *testing.T,
	frontier *Frontier,
	profile crawladmission.AdmissionProfile,
	seed priorityPageSeed,
) {
	t.Helper()
	urls := make([]string, 0, seed.pages)
	for page := range seed.pages {
		urls = append(urls, fmt.Sprintf("https://%s.example/page/%03d", seed.provenance, page))
	}
	seeded := frontier.SeedRunWithPriority(
		context.Background(),
		CrawlRunSeed{
			Requests:   internalRequests(profile, urls...),
			Provenance: []byte(seed.provenance),
			Priority:   seed.priority,
		},
		profile,
		nil,
	)
	if seeded.Queued != seed.pages {
		t.Fatalf("%s queued = %d, want %d", seed.provenance, seeded.Queued, seed.pages)
	}
}

func takePriorityPage(t *testing.T, frontier *Frontier) string {
	t.Helper()
	job := internalReceive(t, frontier)
	provenance := string(job.Provenance)
	frontier.Done(job, false)

	return provenance
}

func TestAutomaticDiscoveryPriorityUsesBoundedBurst(t *testing.T) {
	frontier := NewFrontier(16, nil)
	profile := internalProfile(t)
	seedPriorityPages(t, frontier, profile, normalPageSeed(2))
	seedPriorityPages(t, frontier, profile, automaticPageSeed("automatic", 4))

	want := []string{"automatic", "automatic", "automatic", "normal", "automatic", "normal"}
	for index, provenance := range want {
		if got := takePriorityPage(t, frontier); got != provenance {
			t.Fatalf("dispatch %d = %q, want %q in %v", index, got, provenance, want)
		}
	}
}

func TestAutomaticDiscoveryPriorityRetainsFairnessWithinClass(t *testing.T) {
	frontier := NewFrontier(16, nil, WithValueScorer(func(job crawljob.CrawlJob, _ int) float64 {
		if string(job.Provenance) == "automatic-high" {
			return 100
		}

		return 1
	}))
	profile := internalProfile(t)
	seedPriorityPages(t, frontier, profile, normalPageSeed(1))
	seedPriorityPages(t, frontier, profile, automaticPageSeed("automatic-high", 3))
	seedPriorityPages(t, frontier, profile, automaticPageSeed("automatic-low", 2))

	want := []string{"automatic-high", "automatic-low", "automatic-high", "normal"}
	for index, provenance := range want {
		if got := takePriorityPage(t, frontier); got != provenance {
			t.Fatalf("dispatch %d = %q, want %q in %v", index, got, provenance, want)
		}
	}
	frontier.Cancel([]byte("automatic-high"))
	frontier.Cancel([]byte("automatic-low"))
}

func TestDisabledAutomaticDiscoveryPriorityUsesExistingScoring(t *testing.T) {
	frontier := NewFrontier(8, nil,
		WithAutomaticDiscoveryPriority(false),
		WithValueScorer(func(job crawljob.CrawlJob, _ int) float64 {
			if string(job.Provenance) == "normal" {
				return 100
			}

			return 1
		}),
	)
	profile := internalProfile(t)
	seedPriorityPages(t, frontier, profile, normalPageSeed(1))
	seedPriorityPages(t, frontier, profile, automaticPageSeed("automatic", 1))

	if got := takePriorityPage(t, frontier); got != "normal" {
		t.Fatalf("disabled priority dispatched %q, want scorer-selected normal", got)
	}
	if got := takePriorityPage(t, frontier); got != "automatic" {
		t.Fatalf("remaining dispatch = %q, want automatic", got)
	}
}

func TestUnknownCrawlOrderPriorityRemainsNormal(t *testing.T) {
	frontier := NewFrontier(8, nil, WithValueScorer(func(job crawljob.CrawlJob, _ int) float64 {
		if string(job.Provenance) == "future" {
			return 100
		}

		return 1
	}))
	profile := internalProfile(t)
	seedPriorityPages(t, frontier, profile, priorityPageSeed{
		provenance: "future",
		priority:   yagocrawlcontract.CrawlOrderPriority("future-priority"),
		pages:      1,
	})
	seedPriorityPages(t, frontier, profile, automaticPageSeed("automatic", 1))

	for _, provenance := range []string{"automatic", "future"} {
		if got := takePriorityPage(t, frontier); got != provenance {
			t.Fatalf("dispatch = %q, want %q", got, provenance)
		}
	}
}

func TestAutomaticDiscoveryPriorityIsWorkConserving(t *testing.T) {
	frontier := NewFrontier(16, nil)
	profile := internalProfile(t)
	frontier.Pause([]byte("normal"))
	seedPriorityPages(t, frontier, profile, normalPageSeed(2))
	seedPriorityPages(t, frontier, profile, automaticPageSeed("automatic", 5))

	if got := takePriorityPage(t, frontier); got != "automatic" {
		t.Fatalf("only runnable class dispatched %q, want automatic", got)
	}
	frontier.Resume([]byte("normal"))
	for index, provenance := range []string{"automatic", "automatic", "automatic", "normal"} {
		if got := takePriorityPage(t, frontier); got != provenance {
			t.Fatalf("dispatch %d after resume = %q, want %q", index, got, provenance)
		}
	}
	frontier.Cancel([]byte("normal"))
	frontier.Cancel([]byte("automatic"))
}

func TestAutomaticDiscoveryPriorityDispatchesDueNormalPastRateLimitedAutomatic(t *testing.T) {
	frontier := NewFrontier(8, nil)
	profile := internalProfile(t)
	frontier.SetRate([]byte("automatic"), 1)
	seedPriorityPages(t, frontier, profile, normalPageSeed(1))
	seedPriorityPages(t, frontier, profile, automaticPageSeed("automatic", 2))

	if got := takePriorityPage(t, frontier); got != "automatic" {
		t.Fatalf("first dispatch = %q, want automatic", got)
	}
	if got := takePriorityPage(t, frontier); got != "normal" {
		t.Fatalf("dispatch past rate-limited automatic = %q, want normal", got)
	}
	frontier.Cancel([]byte("automatic"))
}

func TestAutomaticDiscoveryPriorityDispatchesNormalPastHostLimitedAutomatic(t *testing.T) {
	frontier := NewFrontier(8, nil, WithMaxHostConcurrency(1))
	profile := internalProfile(t)
	seedPriorityPages(t, frontier, profile, normalPageSeed(1))
	frontier.SeedRunWithPriority(
		context.Background(),
		CrawlRunSeed{
			Requests: internalRequests(
				profile,
				"https://automatic.example/one",
				"https://automatic.example/two",
			),
			Provenance: []byte("automatic"),
			Priority:   yagocrawlcontract.CrawlOrderPriorityAutomaticDiscovery,
		},
		profile,
		nil,
	)

	automatic := internalReceive(t, frontier)
	if got := string(automatic.Provenance); got != "automatic" {
		t.Fatalf("first dispatch = %q, want automatic", got)
	}
	normal := internalReceive(t, frontier)
	if got := string(normal.Provenance); got != "normal" {
		t.Fatalf("dispatch past host-limited automatic = %q, want normal", got)
	}
	frontier.Done(normal, false)
	frontier.Done(automatic, false)
	frontier.Cancel([]byte("automatic"))
}

func TestAutomaticDiscoveryPriorityToggleIsRaceSafe(t *testing.T) {
	frontier := NewFrontier(32, nil)
	profile := internalProfile(t)
	seedPriorityPages(t, frontier, profile, normalPageSeed(100))
	seedPriorityPages(t, frontier, profile, automaticPageSeed("automatic", 100))

	var toggles sync.WaitGroup
	toggles.Add(1)
	go func() {
		defer toggles.Done()
		for iteration := range 1_000 {
			frontier.SetAutomaticDiscoveryPriority(iteration%2 == 0)
		}
	}()
	for range 100 {
		takePriorityPage(t, frontier)
	}
	toggles.Wait()
	frontier.SetAutomaticDiscoveryPriority(true)
	if got := takePriorityPage(t, frontier); got != "automatic" {
		t.Fatalf("enabled live priority dispatched %q, want automatic", got)
	}
	frontier.Cancel([]byte("normal"))
	frontier.Cancel([]byte("automatic"))
}
