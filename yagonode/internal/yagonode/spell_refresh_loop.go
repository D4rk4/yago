package yagonode

import (
	"context"
	"log/slog"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/spellcheck"
)

const (
	defaultSpellRefreshInterval = 10 * time.Minute
	spellRefreshFailedMessage   = "spell dictionary refresh scan failed"
	// spellMinTermFrequency drops singletons so corrections target words the
	// corpus actually uses repeatedly, not one-off typos or junk tokens.
	spellMinTermFrequency = 2
)

// spellSweeper rebuilds the spelling corrector from the stored documents' titles
// and body text and publishes it for the zero-result recovery to consult. Like
// the host-rank sweep, the vocabulary changes only as crawls land, so a coarse
// interval keeps corrections fresh without rescanning on the query path.
type spellSweeper struct {
	documents documentstore.StoredDocuments
	holder    *spellcheck.Holder
}

var newSpellRefreshTicks = func(interval time.Duration) (<-chan time.Time, func()) {
	ticker := time.NewTicker(interval)

	return ticker.C, ticker.Stop
}

func runSpellRefreshLoop(ctx context.Context, sweeper spellSweeper) {
	sweeper.refreshOnce(ctx)

	ticks, stop := newSpellRefreshTicks(defaultSpellRefreshInterval)
	defer stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticks:
			sweeper.refreshOnce(ctx)
		}
	}
}

func (s spellSweeper) refreshOnce(ctx context.Context) {
	if s.documents == nil || s.holder == nil {
		return
	}

	frequency := map[string]int{}
	err := s.documents.StoredDocuments(ctx, func(doc documentstore.Document) (bool, error) {
		spellcheck.TermFrequencies(frequency, doc.Title)
		spellcheck.TermFrequencies(frequency, doc.ExtractedText)

		return true, nil
	})
	if err != nil {
		slog.WarnContext(ctx, spellRefreshFailedMessage, slog.Any("error", err))

		return
	}

	s.holder.Store(spellcheck.New(prunedVocabulary(frequency)))
}

// prunedVocabulary keeps only terms seen at least spellMinTermFrequency times,
// bounding the dictionary and steering corrections toward common words.
func prunedVocabulary(frequency map[string]int) map[string]int {
	pruned := make(map[string]int, len(frequency))
	for term, count := range frequency {
		if count >= spellMinTermFrequency {
			pruned[term] = count
		}
	}

	return pruned
}
