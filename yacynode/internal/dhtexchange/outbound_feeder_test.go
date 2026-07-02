package dhtexchange

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/D4rk4/yago/yacymodel"
)

type wordSourceScript struct {
	words         []yacymodel.WordPostings
	selectErr     error
	restoreErr    error
	selectCalls   int
	restoreCalls  int
	maxWords      int
	maxPostings   int
	restoredWords []yacymodel.WordPostings
}

func (s *wordSourceScript) SelectOutboundWords(
	_ context.Context,
	maxWords int,
	maxPostings int,
) ([]yacymodel.WordPostings, error) {
	s.selectCalls++
	s.maxWords = maxWords
	s.maxPostings = maxPostings
	if s.selectErr != nil {
		return nil, s.selectErr
	}

	return append([]yacymodel.WordPostings(nil), s.words...), nil
}

func (s *wordSourceScript) RestoreOutboundWords(
	_ context.Context,
	words []yacymodel.WordPostings,
) (int, error) {
	s.restoreCalls++
	s.restoredWords = append([]yacymodel.WordPostings(nil), words...)
	if s.restoreErr != nil {
		return 0, s.restoreErr
	}

	return wordPostingCount(words), nil
}

func TestOutboundFeederSkipsWhenQueueAlreadyHasWork(t *testing.T) {
	t.Parallel()

	queue := NewOutboundQueue()
	queue.add(queueSeed(t, "AAAAAAAAAAAA"), []yacymodel.RWIPosting{
		queuePosting(yacymodel.WordHash("word"), yacymodel.WordHash("url")),
	})
	source := &wordSourceScript{}

	receipt, err := NewOutboundFeeder(
		queue,
		source,
		&URLSet{},
		func(context.Context) []yacymodel.Seed { return nil },
		OutboundFeederConfig{},
	).Feed(context.Background())
	if err != nil {
		t.Fatalf("Feed: %v", err)
	}
	if receipt.State != OutboundFeedSkipped || source.selectCalls != 0 {
		t.Fatalf("receipt/source = %#v/%d", receipt, source.selectCalls)
	}
}

func TestOutboundFeederSelectsAndEnqueuesWords(t *testing.T) {
	t.Parallel()

	word := queueHash(t, "WWWWWWWWWWWW")
	source := &wordSourceScript{words: []yacymodel.WordPostings{{
		WordHash: word,
		Postings: []yacymodel.RWIPosting{
			queuePosting(word, yacymodel.WordHash("url-a")),
			queuePosting(word, yacymodel.WordHash("url-b")),
		},
	}}}
	queue := NewOutboundQueue()

	receipt, err := NewOutboundFeeder(
		queue,
		source,
		&URLSet{},
		func(context.Context) []yacymodel.Seed {
			return []yacymodel.Seed{queueSeed(t, "AAAAAAAAAAAA")}
		},
		OutboundFeederConfig{
			MaxWords:           2,
			MaxPostings:        10,
			Redundancy:         1,
			MinimumPeerAgeDays: -1,
			Now:                func() time.Time { return time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC) },
		},
	).Feed(context.Background())
	if err != nil {
		t.Fatalf("Feed: %v", err)
	}
	if receipt.State != OutboundFeedEnqueued ||
		receipt.SelectedPostings != 2 ||
		receipt.Enqueue.TargetCopies != 2 ||
		queue.PostingCount() != 2 ||
		source.maxWords != 2 ||
		source.maxPostings != 10 ||
		source.restoreCalls != 0 {
		t.Fatalf("receipt/source/queue = %#v/%#v/%d", receipt, source, queue.PostingCount())
	}
}

func TestOutboundFeederDropsRowsWithoutLocalURLMetadata(t *testing.T) {
	t.Parallel()

	word := queueHash(t, "WWWWWWWWWWWW")
	missing := yacymodel.WordHash("missing")
	source := &wordSourceScript{words: []yacymodel.WordPostings{{
		WordHash: word,
		Postings: []yacymodel.RWIPosting{queuePosting(word, missing)},
	}}}

	receipt, err := NewOutboundFeeder(
		NewOutboundQueue(),
		source,
		&URLSet{missing: map[yacymodel.Hash]struct{}{missing: {}}},
		func(context.Context) []yacymodel.Seed {
			return []yacymodel.Seed{queueSeed(t, "AAAAAAAAAAAA")}
		},
		OutboundFeederConfig{Redundancy: 1, MinimumPeerAgeDays: -1},
	).Feed(context.Background())
	if err != nil {
		t.Fatalf("Feed: %v", err)
	}
	if receipt.State != OutboundFeedDropped ||
		receipt.Enqueue.MissingURL != 1 ||
		source.restoreCalls != 0 {
		t.Fatalf("receipt/source = %#v/%#v", receipt, source)
	}
}

func TestOutboundFeederRestoresWhenNoTargetAcceptsRows(t *testing.T) {
	t.Parallel()

	word := queueHash(t, "WWWWWWWWWWWW")
	source := &wordSourceScript{words: []yacymodel.WordPostings{{
		WordHash: word,
		Postings: []yacymodel.RWIPosting{queuePosting(word, yacymodel.WordHash("url"))},
	}}}

	receipt, err := NewOutboundFeeder(
		NewOutboundQueue(),
		source,
		&URLSet{},
		func(context.Context) []yacymodel.Seed { return nil },
		OutboundFeederConfig{Redundancy: 1},
	).Feed(context.Background())
	if err != nil {
		t.Fatalf("Feed: %v", err)
	}
	if receipt.State != OutboundFeedRestored ||
		receipt.RestoredPostings != 1 ||
		source.restoreCalls != 1 {
		t.Fatalf("receipt/source = %#v/%#v", receipt, source)
	}
}

func TestOutboundFeederKeepsMissingURLRowsDroppedDuringRestore(t *testing.T) {
	t.Parallel()

	word := queueHash(t, "WWWWWWWWWWWW")
	missing := yacymodel.WordHash("missing")
	kept := yacymodel.WordHash("kept")
	source := &wordSourceScript{words: []yacymodel.WordPostings{{
		WordHash: word,
		Postings: []yacymodel.RWIPosting{
			queuePosting(word, missing),
			queuePosting(word, kept),
		},
	}}}

	receipt, err := NewOutboundFeeder(
		NewOutboundQueue(),
		source,
		&URLSet{missing: map[yacymodel.Hash]struct{}{missing: {}}},
		func(context.Context) []yacymodel.Seed { return nil },
		OutboundFeederConfig{Redundancy: 1},
	).Feed(context.Background())
	if err != nil {
		t.Fatalf("Feed: %v", err)
	}
	if receipt.State != OutboundFeedRestored ||
		receipt.RestoredPostings != 1 ||
		len(source.restoredWords) != 1 ||
		len(source.restoredWords[0].Postings) != 1 {
		t.Fatalf("receipt/source = %#v/%#v", receipt, source)
	}
	restored, err := source.restoredWords[0].Postings[0].URLHash()
	if err != nil {
		t.Fatalf("URLHash: %v", err)
	}
	if restored.Hash() != kept {
		t.Fatalf("restored url = %s, want %s", restored, kept)
	}
}

func TestOutboundFeederRestoresAndReturnsEnqueueErrors(t *testing.T) {
	t.Parallel()

	word := yacymodel.Hash("bad")
	source := &wordSourceScript{words: []yacymodel.WordPostings{
		{
			WordHash: word,
			Postings: []yacymodel.RWIPosting{
				queuePosting(yacymodel.WordHash("word"), yacymodel.WordHash("url")),
			},
		},
	}}
	queue := NewOutboundQueue()

	receipt, err := NewOutboundFeeder(
		queue,
		source,
		&URLSet{},
		func(context.Context) []yacymodel.Seed {
			return []yacymodel.Seed{queueSeed(t, "AAAAAAAAAAAA")}
		},
		OutboundFeederConfig{Redundancy: 1},
	).Feed(context.Background())
	if err == nil {
		t.Fatal("expected enqueue error")
	}
	if receipt.State != OutboundFeedRestored ||
		receipt.RestoredPostings != 1 ||
		queue.PostingCount() != 0 {
		t.Fatalf("receipt/queue = %#v/%d", receipt, queue.PostingCount())
	}
}

func TestOutboundFeederReportsSelectAndRestoreErrors(t *testing.T) {
	t.Parallel()

	_, err := NewOutboundFeeder(
		NewOutboundQueue(),
		&wordSourceScript{selectErr: errors.New("select failed")},
		&URLSet{},
		func(context.Context) []yacymodel.Seed { return nil },
		OutboundFeederConfig{},
	).Feed(context.Background())
	if err == nil {
		t.Fatal("expected select error")
	}

	word := queueHash(t, "WWWWWWWWWWWW")
	_, err = NewOutboundFeeder(
		NewOutboundQueue(),
		&wordSourceScript{
			words: []yacymodel.WordPostings{{
				WordHash: word,
				Postings: []yacymodel.RWIPosting{
					queuePosting(word, yacymodel.WordHash("url")),
				},
			}},
			restoreErr: errors.New("restore failed"),
		},
		&URLSet{},
		func(context.Context) []yacymodel.Seed { return nil },
		OutboundFeederConfig{Redundancy: 1},
	).Feed(context.Background())
	if err == nil {
		t.Fatal("expected restore error")
	}
}

func TestOutboundFeederReportsEmptySelectionAndNormalizesConfig(t *testing.T) {
	t.Parallel()

	source := &wordSourceScript{}
	receipt, err := NewOutboundFeeder(
		NewOutboundQueue(),
		source,
		&URLSet{},
		func(context.Context) []yacymodel.Seed { return nil },
		OutboundFeederConfig{MaxPostings: MaxChunkPostings + 1},
	).Feed(context.Background())
	if err != nil {
		t.Fatalf("Feed: %v", err)
	}
	if receipt.State != OutboundFeedEmpty ||
		source.maxWords != 1 ||
		source.maxPostings != MaxChunkPostings {
		t.Fatalf("receipt/source = %#v/%#v", receipt, source)
	}
}
