package rwi

import (
	"errors"
	"slices"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type scriptedMutationFailure struct {
	remainingSuccessfulMutations int
	err                          error
}

func TestRecoverOutboundConvergesAfterPartialJournalMutation(t *testing.T) {
	_, index, receiver, _, engine := openScriptedRWI(t, fakeURLDirectory{})
	postings := outboundDurabilityPostings()
	if _, err := receiver.Receive(t.Context(), postings); err != nil {
		t.Fatalf("receive postings: %v", err)
	}

	journalFailure := errors.New("injected later journal mutation failure")
	engine.putFailures[OutboundSelectionBucket] = &scriptedMutationFailure{
		remainingSuccessfulMutations: 1,
		err:                          journalFailure,
	}
	store := outboundStore(t, index)
	if _, err := store.SelectOutbound(
		t.Context(),
		OutboundSelectionConfig{MaxWords: 2, MaxPostings: 2},
	); !errors.Is(err, journalFailure) {
		t.Fatalf("select outbound error = %v, want %v", err, journalFailure)
	}

	assertOutboundJournalKeys(t, engine, outboundDurabilityKeys(t, postings)[:1])
	assertOutboundLivePostings(t, index, postings)
	delete(engine.putFailures, OutboundSelectionBucket)

	recovered, err := store.RecoverOutbound(t.Context())
	if err != nil {
		t.Fatalf("recover outbound: %v", err)
	}
	if recovered != 1 {
		t.Fatalf("recovered = %d, want 1", recovered)
	}
	assertOutboundLivePostings(t, index, postings)
	assertOutboundJournalKeys(t, engine, nil)
	assertOutboundRecoveryIdempotent(t, store)
}

func TestRecoverOutboundConvergesAfterPartialLiveDeletion(t *testing.T) {
	_, index, receiver, _, engine := openScriptedRWI(t, fakeURLDirectory{})
	postings := outboundDurabilityPostings()
	if _, err := receiver.Receive(t.Context(), postings); err != nil {
		t.Fatalf("receive postings: %v", err)
	}

	deleteFailure := errors.New("injected later live mutation failure")
	engine.deleteFailures[PostingsBucket] = &scriptedMutationFailure{
		remainingSuccessfulMutations: 1,
		err:                          deleteFailure,
	}
	store := outboundStore(t, index)
	if _, err := store.SelectOutbound(
		t.Context(),
		OutboundSelectionConfig{MaxWords: 2, MaxPostings: 2},
	); !errors.Is(err, deleteFailure) {
		t.Fatalf("select outbound error = %v, want %v", err, deleteFailure)
	}

	keys := outboundDurabilityKeys(t, postings)
	assertOutboundJournalKeys(t, engine, keys)
	assertOutboundLiveKeys(t, engine, keys[1:])
	delete(engine.deleteFailures, PostingsBucket)

	releaseFailure := errors.New("injected journal release failure")
	engine.deleteFailures[OutboundSelectionBucket] = &scriptedMutationFailure{
		err: releaseFailure,
	}
	if _, err := store.RecoverOutbound(t.Context()); !errors.Is(err, releaseFailure) {
		t.Fatalf("first recovery error = %v, want %v", err, releaseFailure)
	}
	assertOutboundLivePostings(t, index, postings)
	assertOutboundJournalKeys(t, engine, keys)
	delete(engine.deleteFailures, OutboundSelectionBucket)

	recovered, err := store.RecoverOutbound(t.Context())
	if err != nil {
		t.Fatalf("recover outbound: %v", err)
	}
	if recovered != len(postings) {
		t.Fatalf("recovered = %d, want %d", recovered, len(postings))
	}
	assertOutboundLivePostings(t, index, postings)
	assertOutboundJournalKeys(t, engine, nil)
	assertOutboundRecoveryIdempotent(t, store)
}

func outboundDurabilityPostings() []yagomodel.RWIPosting {
	return []yagomodel.RWIPosting{
		postingWithHashes(yagomodel.Hash("AAAAAAAAAAAA"), yagomodel.Hash("CCCCCCCCCCCC")),
		postingWithHashes(yagomodel.Hash("BBBBBBBBBBBB"), yagomodel.Hash("DDDDDDDDDDDD")),
	}
}

func outboundDurabilityKeys(t *testing.T, postings []yagomodel.RWIPosting) []vault.Key {
	t.Helper()
	keys := make([]vault.Key, 0, len(postings))
	for _, posting := range postings {
		url, err := posting.URLHash()
		if err != nil {
			t.Fatalf("posting URL hash: %v", err)
		}
		keys = append(keys, postingKey(posting.WordHash, url.Hash()))
	}
	slices.SortFunc(keys, slices.Compare)

	return keys
}

func assertOutboundLivePostings(
	t *testing.T,
	index PostingIndex,
	postings []yagomodel.RWIPosting,
) {
	t.Helper()
	for _, posting := range postings {
		url, err := posting.URLHash()
		if err != nil {
			t.Fatalf("posting URL hash: %v", err)
		}
		assertStoredPosting(t, t.Context(), index, posting.WordHash, url.Hash())
	}
}

func assertOutboundLiveKeys(t *testing.T, engine *scriptedEngine, expected []vault.Key) {
	t.Helper()
	assertOutboundBucketKeys(t, engine, PostingsBucket, expected)
}

func assertOutboundJournalKeys(t *testing.T, engine *scriptedEngine, expected []vault.Key) {
	t.Helper()
	assertOutboundBucketKeys(t, engine, OutboundSelectionBucket, expected)
}

func assertOutboundBucketKeys(
	t *testing.T,
	engine *scriptedEngine,
	bucket vault.Name,
	expected []vault.Key,
) {
	t.Helper()
	actual := make([]vault.Key, 0, len(engine.buckets[bucket]))
	for key := range engine.buckets[bucket] {
		actual = append(actual, vault.Key(key))
	}
	slices.SortFunc(actual, slices.Compare)
	if !slices.EqualFunc(actual, expected, slices.Equal) {
		t.Fatalf("bucket %s keys = %q, want %q", bucket, actual, expected)
	}
}

func assertOutboundRecoveryIdempotent(t *testing.T, store OutboundPostingStore) {
	t.Helper()
	recovered, err := store.RecoverOutbound(t.Context())
	if err != nil || recovered != 0 {
		t.Fatalf("second recovery = %d, %v; want 0, nil", recovered, err)
	}
}
