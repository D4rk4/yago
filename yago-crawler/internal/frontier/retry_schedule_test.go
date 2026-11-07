package frontier_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/D4rk4/yago/yago-crawler/internal/crawljob"
	"github.com/D4rk4/yago/yago-crawler/internal/frontier"
)

func TestDeferConsumesPageWithoutActiveRun(t *testing.T) {
	queue := frontier.NewFrontier(1, nil)
	work := crawljob.CrawlJob{
		RunID: uuid.New(),
		URL:   "https://example.test/page",
	}
	consumed, scheduled := queue.Defer(t.Context(), work, time.Now().Add(time.Minute))
	if !consumed || scheduled {
		t.Fatalf(
			"defer without active run = %t/%t, want consumed and unscheduled",
			consumed,
			scheduled,
		)
	}
}
