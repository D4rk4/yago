package yagonode

import (
	"context"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

type timedCorpus struct {
	clock    *time.Time
	scanTime time.Duration
}

func (c timedCorpus) StoredDocuments(
	context.Context,
	func(documentstore.Document) (bool, error),
) error {
	*c.clock = c.clock.Add(c.scanTime)

	return nil
}

func TestCorpusSignalRefreshRestKeepsIntervalAfterShortScans(t *testing.T) {
	for _, scanTime := range []time.Duration{0, time.Second, defaultCorpusSignalRefreshInterval / corpusSignalRestPerScanTime} {
		if rest := corpusSignalRefreshRest(scanTime); rest != defaultCorpusSignalRefreshInterval {
			t.Fatalf(
				"rest after %v scan = %v, want %v",
				scanTime,
				rest,
				defaultCorpusSignalRefreshInterval,
			)
		}
	}
}

func TestCorpusSignalRefreshRestGrowsWithLongScans(t *testing.T) {
	if rest := corpusSignalRefreshRest(6 * time.Minute); rest != 54*time.Minute {
		t.Fatalf("rest after 6m scan = %v, want 54m", rest)
	}
}

func TestCorpusSignalRefreshLoopRestsInProportionToScanTime(t *testing.T) {
	oldDelay := newCorpusSignalRefreshDelay
	t.Cleanup(func() { newCorpusSignalRefreshDelay = oldDelay })
	ctx, cancel := context.WithCancel(t.Context())
	rests := make(chan time.Duration, 1)
	newCorpusSignalRefreshDelay = func(interval time.Duration) (<-chan time.Time, func()) {
		rests <- interval
		cancel()

		return nil, func() {}
	}
	clock := time.Unix(1_700_000_000, 0)
	runCorpusSignalRefreshLoop(ctx, &corpusSignalRefresh{
		documents: timedCorpus{clock: &clock, scanTime: 20 * time.Minute},
		readTime:  func() time.Time { return clock },
	})

	if rest := <-rests; rest != 3*time.Hour {
		t.Fatalf("rest after 20m scan = %v, want 3h", rest)
	}
}
