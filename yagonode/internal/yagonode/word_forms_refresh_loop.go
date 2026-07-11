package yagonode

import (
	"context"
	"log/slog"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
	"github.com/D4rk4/yago/yagonode/internal/spellcheck"
	"github.com/D4rk4/yago/yagonode/internal/wordforms"
)

const (
	defaultWordFormsRefreshInterval = 10 * time.Minute
	wordFormsRefreshFailedMessage   = "word-form expander refresh scan failed"
	wordFormsVocabularyTerms        = 32_768
)

// wordFormsSweeper rebuilds the swarm morphology expander from the stored
// documents' vocabulary, grouping surface forms by the per-language stem so a
// single-word swarm query can also search its inflections. Like the other
// vocabulary sweeps it runs on a coarse interval off the query path.
type wordFormsSweeper struct {
	documents documentstore.StoredDocuments
	holder    *wordforms.Holder
}

var newWordFormsRefreshTicks = func(interval time.Duration) (<-chan time.Time, func()) {
	ticker := time.NewTicker(interval)

	return ticker.C, ticker.Stop
}

func runWordFormsRefreshLoop(ctx context.Context, sweeper wordFormsSweeper) {
	sweeper.refreshOnce(ctx)

	ticks, stop := newWordFormsRefreshTicks(defaultWordFormsRefreshInterval)
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

func (s wordFormsSweeper) refreshOnce(ctx context.Context) {
	if s.documents == nil || s.holder == nil {
		return
	}

	frequency := spellcheck.NewFrequencySynopsis(wordFormsVocabularyTerms)
	err := s.documents.StoredDocuments(ctx, func(doc documentstore.Document) (bool, error) {
		frequency.ObserveText(doc.Title)
		frequency.ObserveText(doc.ExtractedText)

		return true, nil
	})
	if err != nil {
		slog.WarnContext(ctx, wordFormsRefreshFailedMessage, slog.Any("error", err))

		return
	}

	s.holder.Store(wordforms.New(frequency.Frequencies(), searchindex.StemWord))
}
