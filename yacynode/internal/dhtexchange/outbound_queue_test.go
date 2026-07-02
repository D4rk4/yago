package dhtexchange

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/D4rk4/yago/yacymodel"
)

type URLSet struct {
	missing map[yacymodel.Hash]struct{}
	err     error
	calls   int
	got     []yacymodel.Hash
}

func (s *URLSet) MissingURLs(
	_ context.Context,
	hashes []yacymodel.Hash,
) ([]yacymodel.Hash, error) {
	s.calls++
	s.got = append([]yacymodel.Hash(nil), hashes...)
	if s.err != nil {
		return nil, s.err
	}

	missing := make([]yacymodel.Hash, 0)
	for _, hash := range hashes {
		if _, ok := s.missing[hash]; ok {
			missing = append(missing, hash)
		}
	}

	return missing, nil
}

func queueHash(tb testing.TB, raw string) yacymodel.Hash {
	tb.Helper()

	hash, err := yacymodel.ParseHash(raw)
	if err != nil {
		tb.Fatalf("parse hash %q: %v", raw, err)
	}

	return hash
}

func queueSeed(tb testing.TB, raw string) yacymodel.Seed {
	tb.Helper()

	host, err := yacymodel.ParseHost("192.0.2.1")
	if err != nil {
		tb.Fatalf("parse host: %v", err)
	}
	flags := yacymodel.ZeroFlags().Set(yacymodel.FlagAcceptRemoteIndex, true)

	return yacymodel.Seed{
		Hash:  queueHash(tb, raw),
		IP:    yacymodel.Some(host),
		Port:  yacymodel.Some(yacymodel.Port(8090)),
		Flags: yacymodel.Some(flags),
		BirthDate: yacymodel.Some(
			yacymodel.NewSeedBirthDateUTC(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)),
		),
	}
}

func queuePosting(word, url yacymodel.Hash) yacymodel.RWIPosting {
	return yacymodel.RWIPosting{
		WordHash: word,
		Properties: map[string]string{
			yacymodel.ColURLHash: url.String(),
		},
	}
}

func TestOutboundQueueEnqueuesEligiblePostingsForTargets(t *testing.T) {
	t.Parallel()

	word := yacymodel.WordHash("word")
	urlA := yacymodel.WordHash("url-a")
	urlB := yacymodel.WordHash("url-b")
	missing := yacymodel.WordHash("missing")
	disabled := queueSeed(t, "CCCCCCCCCCCC")
	disabled.Flags = yacymodel.Some(yacymodel.ZeroFlags())
	urls := &URLSet{missing: map[yacymodel.Hash]struct{}{missing: {}}}
	queue := NewOutboundQueue()

	receipt, err := queue.EnqueueWord(
		context.Background(),
		urls,
		[]yacymodel.Seed{
			queueSeed(t, "AAAAAAAAAAAA"),
			queueSeed(t, "BBBBBBBBBBBB"),
			disabled,
		},
		yacymodel.WordPostings{
			WordHash: word,
			Postings: []yacymodel.RWIPosting{
				queuePosting(word, urlA),
				queuePosting(word, missing),
				{WordHash: word, Properties: map[string]string{yacymodel.ColURLHash: "bad"}},
				queuePosting(word, urlB),
			},
		},
		EnqueueConfig{Redundancy: 2, MinimumPeerAgeDays: -1},
	)
	if err != nil {
		t.Fatalf("EnqueueWord: %v", err)
	}

	if receipt.AcceptedPostings != 2 ||
		receipt.MissingURL != 1 ||
		receipt.BadPostings != 1 ||
		receipt.TargetCopies != 4 ||
		receipt.OverflowCopies != 0 ||
		receipt.TouchedChunks != 2 {
		t.Fatalf("receipt = %#v", receipt)
	}
	if queue.Len() != 2 || queue.PostingCount() != 4 || urls.calls != 1 {
		t.Fatalf(
			"queue len/count/url calls = %d/%d/%d",
			queue.Len(),
			queue.PostingCount(),
			urls.calls,
		)
	}
}

func TestOutboundQueueUsesVerticalURLPartitions(t *testing.T) {
	t.Parallel()

	word := queueHash(t, "AAAAAAAAAAAA")
	lowURL := queueHash(t, "AAAAAAAAAAAA")
	highURL := queueHash(t, "__________AA")
	queue := NewOutboundQueue()

	receipt, err := queue.EnqueueWord(
		context.Background(),
		&URLSet{},
		[]yacymodel.Seed{
			queueSeed(t, "AAAAAAAAAAAA"),
			queueSeed(t, "__________AA"),
		},
		yacymodel.WordPostings{
			WordHash: word,
			Postings: []yacymodel.RWIPosting{
				queuePosting(word, lowURL),
				queuePosting(word, highURL),
			},
		},
		EnqueueConfig{Redundancy: 1, PartitionExponent: 1, MinimumPeerAgeDays: -1},
	)
	if err != nil {
		t.Fatalf("EnqueueWord: %v", err)
	}
	if receipt.TargetCopies != 2 || queue.Len() != 2 {
		t.Fatalf("receipt/len = %#v/%d", receipt, queue.Len())
	}

	first, ok := queue.DequeueLargest()
	if !ok {
		t.Fatal("first dequeue failed")
	}
	second, ok := queue.DequeueLargest()
	if !ok {
		t.Fatal("second dequeue failed")
	}
	got := map[yacymodel.Hash]yacymodel.Hash{
		first.Peer.Hash:  yacymodel.Hash(first.Postings[0].Properties[yacymodel.ColURLHash]),
		second.Peer.Hash: yacymodel.Hash(second.Postings[0].Properties[yacymodel.ColURLHash]),
	}
	want := map[yacymodel.Hash]yacymodel.Hash{
		queueHash(t, "AAAAAAAAAAAA"): lowURL,
		queueHash(t, "__________AA"): highURL,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("partitioned chunks = %#v, want %#v", got, want)
	}
	if _, ok := queue.DequeueLargest(); ok {
		t.Fatal("expected queue to be empty")
	}
}

func TestOutboundQueueCapsChunkAndReportsOverflow(t *testing.T) {
	t.Parallel()

	word := yacymodel.WordHash("word")
	postings := make([]yacymodel.RWIPosting, 0, MaxChunkPostings+1)
	for i := range MaxChunkPostings + 1 {
		postings = append(
			postings,
			queuePosting(word, yacymodel.WordHash(fmt.Sprintf("url-%d", i))),
		)
	}
	queue := NewOutboundQueue()

	receipt, err := queue.EnqueueWord(
		context.Background(),
		&URLSet{},
		[]yacymodel.Seed{queueSeed(t, "AAAAAAAAAAAA")},
		yacymodel.WordPostings{WordHash: word, Postings: postings},
		EnqueueConfig{Redundancy: 1, MinimumPeerAgeDays: -1},
	)
	if err != nil {
		t.Fatalf("EnqueueWord: %v", err)
	}
	if receipt.TargetCopies != MaxChunkPostings || receipt.OverflowCopies != 1 {
		t.Fatalf("receipt = %#v", receipt)
	}
	if queue.PostingCount() != MaxChunkPostings {
		t.Fatalf("PostingCount = %d, want %d", queue.PostingCount(), MaxChunkPostings)
	}

	again, err := queue.EnqueueWord(
		context.Background(),
		&URLSet{},
		[]yacymodel.Seed{queueSeed(t, "AAAAAAAAAAAA")},
		yacymodel.WordPostings{
			WordHash: word,
			Postings: []yacymodel.RWIPosting{queuePosting(word, yacymodel.WordHash("overflow"))},
		},
		EnqueueConfig{Redundancy: 1, MinimumPeerAgeDays: -1},
	)
	if err != nil {
		t.Fatalf("second EnqueueWord: %v", err)
	}
	if again.TargetCopies != 0 || again.OverflowCopies != 1 {
		t.Fatalf("second receipt = %#v", again)
	}
}

func TestOutboundQueueDequeueLargestRemovesLargestChunk(t *testing.T) {
	t.Parallel()

	word := yacymodel.WordHash("word")
	queue := NewOutboundQueue()
	queue.add(
		queueSeed(t, "BBBBBBBBBBBB"),
		[]yacymodel.RWIPosting{queuePosting(word, yacymodel.WordHash("b"))},
	)
	queue.add(queueSeed(t, "AAAAAAAAAAAA"), []yacymodel.RWIPosting{
		queuePosting(word, yacymodel.WordHash("a1")),
		queuePosting(word, yacymodel.WordHash("a2")),
	})

	chunk, ok := queue.DequeueLargest()
	if !ok {
		t.Fatal("DequeueLargest returned empty")
	}
	if chunk.Peer.Hash != queueHash(t, "AAAAAAAAAAAA") || len(chunk.Postings) != 2 {
		t.Fatalf("chunk = %#v", chunk)
	}
	chunk.Postings[0] = queuePosting(word, yacymodel.WordHash("mutated"))
	if queue.PostingCount() != 1 {
		t.Fatalf("queue retained caller mutation or wrong count: %d", queue.PostingCount())
	}
}

func TestOutboundQueueDequeueLargestReadySkipsDelayedPeers(t *testing.T) {
	t.Parallel()

	word := yacymodel.WordHash("word")
	delayed := queueHash(t, "AAAAAAAAAAAA")
	ready := queueHash(t, "BBBBBBBBBBBB")
	queue := NewOutboundQueue()
	queue.add(queueSeed(t, delayed.String()), []yacymodel.RWIPosting{
		queuePosting(word, yacymodel.WordHash("a1")),
		queuePosting(word, yacymodel.WordHash("a2")),
	})
	queue.add(queueSeed(t, ready.String()), []yacymodel.RWIPosting{
		queuePosting(word, yacymodel.WordHash("b")),
	})

	chunk, ok := queue.DequeueLargestReady(func(peer yacymodel.Hash) bool {
		return peer == ready
	})
	if !ok || chunk.Peer.Hash != ready || len(chunk.Postings) != 1 {
		t.Fatalf("chunk = %#v ok=%t", chunk, ok)
	}
	if queue.PostingCount() != 2 {
		t.Fatalf("queue postings = %d, want delayed peer retained", queue.PostingCount())
	}
	if _, ok := queue.DequeueLargestReady(func(yacymodel.Hash) bool { return false }); ok {
		t.Fatal("expected no ready chunk")
	}
	chunk, ok = queue.DequeueLargestReady(nil)
	if !ok || chunk.Peer.Hash != delayed || len(chunk.Postings) != 2 {
		t.Fatalf("nil ready chunk = %#v ok=%t", chunk, ok)
	}
}

func TestOutboundQueueDefaultAgeGateFiltersYoungPeers(t *testing.T) {
	t.Parallel()

	word := yacymodel.WordHash("word")
	young := queueSeed(t, "AAAAAAAAAAAA")
	young.BirthDate = yacymodel.Some(
		yacymodel.NewSeedBirthDateUTC(time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)),
	)
	queue := NewOutboundQueue()

	receipt, err := queue.EnqueueWord(
		context.Background(),
		&URLSet{},
		[]yacymodel.Seed{young},
		yacymodel.WordPostings{
			WordHash: word,
			Postings: []yacymodel.RWIPosting{queuePosting(word, yacymodel.WordHash("url"))},
		},
		EnqueueConfig{
			Redundancy: 1,
			Now:        time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC),
		},
	)
	if err != nil {
		t.Fatalf("EnqueueWord: %v", err)
	}
	if receipt.AcceptedPostings != 1 || receipt.TargetCopies != 0 || queue.Len() != 0 {
		t.Fatalf("receipt/len = %#v/%d", receipt, queue.Len())
	}
}

func TestOutboundQueueReturnsLookupAndPartitionErrors(t *testing.T) {
	t.Parallel()

	word := yacymodel.WordHash("word")
	urls := &URLSet{err: errors.New("storage down")}
	_, err := NewOutboundQueue().EnqueueWord(
		context.Background(),
		urls,
		[]yacymodel.Seed{queueSeed(t, "AAAAAAAAAAAA")},
		yacymodel.WordPostings{
			WordHash: word,
			Postings: []yacymodel.RWIPosting{queuePosting(word, yacymodel.WordHash("url"))},
		},
		EnqueueConfig{Redundancy: 1},
	)
	if err == nil {
		t.Fatal("expected lookup error")
	}

	_, err = NewOutboundQueue().EnqueueWord(
		context.Background(),
		&URLSet{},
		[]yacymodel.Seed{queueSeed(t, "AAAAAAAAAAAA")},
		yacymodel.WordPostings{
			WordHash: yacymodel.Hash("bad"),
			Postings: []yacymodel.RWIPosting{queuePosting(word, yacymodel.WordHash("url"))},
		},
		EnqueueConfig{Redundancy: 1},
	)
	if err == nil {
		t.Fatal("expected partition error")
	}

	_, err = NewOutboundQueue().EnqueueWord(
		context.Background(),
		&URLSet{},
		[]yacymodel.Seed{queueSeed(t, "AAAAAAAAAAAA")},
		yacymodel.WordPostings{
			WordHash: word,
			Postings: []yacymodel.RWIPosting{queuePosting(word, yacymodel.WordHash("url"))},
		},
		EnqueueConfig{Redundancy: 1, PartitionExponent: -1},
	)
	if err == nil {
		t.Fatal("expected exponent error")
	}
}

func TestOutboundQueueSkipsURLLookupWhenNoPostingsCanBeSent(t *testing.T) {
	t.Parallel()

	queue := NewOutboundQueue()
	urls := &URLSet{}
	receipt, err := queue.EnqueueWord(
		context.Background(),
		urls,
		[]yacymodel.Seed{queueSeed(t, "AAAAAAAAAAAA")},
		yacymodel.WordPostings{
			WordHash: yacymodel.WordHash("word"),
			Postings: []yacymodel.RWIPosting{
				{
					WordHash:   yacymodel.WordHash("word"),
					Properties: map[string]string{yacymodel.ColURLHash: "bad"},
				},
			},
		},
		EnqueueConfig{Redundancy: 1},
	)
	if err != nil {
		t.Fatalf("EnqueueWord: %v", err)
	}
	if receipt.BadPostings != 1 || receipt.TargetCopies != 0 || urls.calls != 0 {
		t.Fatalf("receipt/url calls = %#v/%d", receipt, urls.calls)
	}
	if added := queue.add(queueSeed(t, "AAAAAAAAAAAA"), nil); added != 0 {
		t.Fatalf("empty add = %d, want 0", added)
	}
}
