package contentcluster

import (
	"context"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"slices"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/boltvault"
	"github.com/D4rk4/yago/yagonode/internal/memvault"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func openMemoryIndex(t *testing.T, limits Limits) (*Index, *vault.Vault) {
	t.Helper()
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("open memory vault: %v", err)
	}
	t.Cleanup(func() { _ = v.Close() })
	index, err := Open(v, limits)
	if err != nil {
		t.Fatalf("open content clusters: %v", err)
	}

	return index, v
}

func replaceEvidence(t *testing.T, index *Index, evidence Evidence) Assignment {
	t.Helper()
	assignment, err := index.Replace(t.Context(), evidence)
	if err != nil {
		t.Fatalf("replace %q: %v", evidence.URL, err)
	}

	return assignment
}

func lookupAssignment(t *testing.T, index *Index, url string) Assignment {
	t.Helper()
	assignment, found, err := index.Lookup(t.Context(), url)
	if err != nil {
		t.Fatalf("lookup %q: %v", url, err)
	}
	if !found {
		t.Fatalf("lookup %q did not find an assignment", url)
	}

	return assignment
}

func TestExactHashWinsBeforeNearCandidate(t *testing.T) {
	limits := DefaultLimits()
	limits.ShingleWords = 2
	limits.MinimumJaccard = 0.5
	index, _ := openMemoryIndex(t, limits)
	exact := replaceEvidence(t, index, Evidence{
		URL:         "https://exact.example/page",
		ContentHash: "shared-hash",
		Text:        "mercury venus earth mars jupiter saturn uranus neptune",
	})
	near := replaceEvidence(t, index, Evidence{
		URL:         "https://near.example/page",
		ContentHash: "near-hash",
		Text:        "alpha beta gamma delta epsilon zeta eta theta",
	})
	query := replaceEvidence(t, index, Evidence{
		URL:         "https://query.example/page",
		ContentHash: "shared-hash",
		Text:        "alpha beta gamma delta epsilon zeta eta theta",
	})
	if query.ClusterID != exact.ClusterID {
		t.Fatalf("exact cluster = %q, want %q", query.ClusterID, exact.ClusterID)
	}
	if query.ClusterID == near.ClusterID {
		t.Fatal("exact match incorrectly joined the LSH candidate")
	}
}

func TestNearMatchRequiresBoundedJaccard(t *testing.T) {
	limits := DefaultLimits()
	limits.ShingleWords = 2
	limits.MinimumJaccard = 0.75
	index, v := openMemoryIndex(t, limits)
	baseText := "alpha beta gamma delta epsilon zeta eta theta"
	base := replaceEvidence(t, index, Evidence{
		URL:         "https://base.example/page",
		ContentHash: "base-hash",
		Text:        baseText,
	})
	near := replaceEvidence(t, index, Evidence{
		URL:         "https://near.example/page",
		ContentHash: "near-hash",
		Text:        baseText,
	})
	if near.ClusterID != base.ClusterID {
		t.Fatalf("near cluster = %q, want %q", near.ClusterID, base.ClusterID)
	}
	distantInput := Evidence{
		URL:         "https://distant.example/page",
		ContentHash: "distant-hash",
		Text:        "one two three four five six seven eight",
	}
	prepared, err := prepareEvidence(t.Context(), limits, distantInput)
	if err != nil {
		t.Fatalf("prepare distant evidence: %v", err)
	}
	err = v.Update(t.Context(), func(tx *vault.Txn) error {
		return index.addPosting(
			tx,
			index.bandBuckets,
			bandKey(0, prepared.Bands[0]),
			"https://base.example/page",
		)
	})
	if err != nil {
		t.Fatalf("force LSH candidate: %v", err)
	}
	distant := replaceEvidence(t, index, distantInput)
	if distant.ClusterID == base.ClusterID {
		t.Fatal("low-Jaccard LSH candidate was merged")
	}
}

func TestRepresentativeChoiceIsIndependentOfArrival(t *testing.T) {
	evidence := []Evidence{
		{
			URL:              "https://z.example",
			ContentHash:      "same",
			Text:             "short",
			Quality:          100,
			InboundAuthority: 100,
		},
		{
			URL:                "https://y.example",
			ContentHash:        "same",
			Text:               "short",
			CanonicalPreferred: true,
			Quality:            1,
		},
		{
			URL:                "https://x.example",
			ContentHash:        "same",
			Text:               "short",
			CanonicalPreferred: true,
			Quality:            2,
		},
		{
			URL:                "https://w.example",
			ContentHash:        "same",
			Text:               "short",
			CanonicalPreferred: true,
			Quality:            2,
			InboundAuthority:   1,
		},
		{
			URL:                "https://a.example",
			ContentHash:        "same",
			Text:               "short",
			CanonicalPreferred: true,
			Quality:            2,
			InboundAuthority:   1,
		},
	}
	permutations := permutations(len(evidence))
	for _, order := range permutations {
		index, _ := openMemoryIndex(t, Limits{})
		var final Assignment
		for _, position := range order {
			final = replaceEvidence(t, index, evidence[position])
		}
		if final.RepresentativeURL != "https://a.example" {
			t.Fatalf("order %v representative = %q", order, final.RepresentativeURL)
		}
		for _, item := range evidence {
			if got := lookupAssignment(
				t,
				index,
				item.URL,
			); got.RepresentativeURL != "https://a.example" {
				t.Fatalf("order %v lookup representative = %q", order, got.RepresentativeURL)
			}
		}
	}
}

func TestRecrawlAndDeleteRetainStableMembership(t *testing.T) {
	index, _ := openMemoryIndex(t, Limits{})
	first := Evidence{URL: "https://a.example", ContentHash: "same", Text: "short", Quality: 1}
	second := Evidence{URL: "https://b.example", ContentHash: "same", Text: "short", Quality: 2}
	firstAssignment := replaceEvidence(t, index, first)
	secondAssignment := replaceEvidence(t, index, second)
	if firstAssignment.ClusterID != secondAssignment.ClusterID {
		t.Fatal("exact duplicates were not clustered")
	}
	if got := replaceEvidence(t, index, second); got != secondAssignment {
		t.Fatalf("idempotent replacement = %+v, want %+v", got, secondAssignment)
	}
	first.ContentHash = "changed"
	first.Text = "entirely changed content"
	changed := replaceEvidence(t, index, first)
	if changed.ClusterID == secondAssignment.ClusterID {
		t.Fatal("changed recrawl remained in its old cluster")
	}
	if got := lookupAssignment(t, index, second.URL); got.ClusterID != secondAssignment.ClusterID {
		t.Fatalf("surviving cluster ID = %q, want %q", got.ClusterID, secondAssignment.ClusterID)
	}
	first.ContentHash = "same"
	first.Text = "short"
	rejoined := replaceEvidence(t, index, first)
	if rejoined.ClusterID != secondAssignment.ClusterID {
		t.Fatalf("rejoined cluster = %q, want %q", rejoined.ClusterID, secondAssignment.ClusterID)
	}
	deleted, err := index.Delete(t.Context(), second.URL)
	if err != nil || !deleted {
		t.Fatalf("delete representative = %v, %v", deleted, err)
	}
	survivor := lookupAssignment(t, index, first.URL)
	if survivor.ClusterID != secondAssignment.ClusterID || survivor.RepresentativeURL != first.URL {
		t.Fatalf("survivor = %+v", survivor)
	}
	deleted, err = index.Delete(t.Context(), second.URL)
	if err != nil || deleted {
		t.Fatalf("repeat delete = %v, %v", deleted, err)
	}
	deleted, err = index.Delete(t.Context(), first.URL)
	if err != nil || !deleted {
		t.Fatalf("delete last member = %v, %v", deleted, err)
	}
	if _, found, err := index.Lookup(t.Context(), first.URL); err != nil || found {
		t.Fatalf("lookup deleted URL = %v, %v", found, err)
	}
	reinserted := replaceEvidence(t, index, first)
	if reinserted.ClusterID != stableClusterID(first.URL, first.ContentHash) {
		t.Fatalf("reinserted cluster = %q", reinserted.ClusterID)
	}
}

func TestDeleteResetsResultAcrossChangedStateReplay(t *testing.T) {
	engine := newClusterFaultEngine()
	store, err := vault.New(engine)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	index, err := Open(store, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	evidence := Evidence{
		URL:         "https://replay.example/page",
		ContentHash: "replayed",
		Text:        "replayed content",
	}
	if _, err := index.Replace(t.Context(), evidence); err != nil {
		t.Fatal(err)
	}
	engine.replayUpdate = func(engine *clusterFaultEngine) {
		delete(engine.buckets[fingerprintBucketName], evidence.URL)
	}
	deleted, err := index.Delete(t.Context(), evidence.URL)
	if err != nil || deleted {
		t.Fatalf("replayed delete = %v/%v, want false", deleted, err)
	}
}

func TestPostingAndClusterLimitsKeepEveryURL(t *testing.T) {
	limits := DefaultLimits()
	limits.MaximumBucketMembers = 2
	limits.MaximumClusterMembers = 2
	limits.MaximumCandidates = 1
	index, v := openMemoryIndex(t, limits)
	first := replaceEvidence(
		t,
		index,
		Evidence{URL: "https://z.example", ContentHash: "same", Text: "short"},
	)
	second := replaceEvidence(
		t,
		index,
		Evidence{URL: "https://y.example", ContentHash: "same", Text: "short"},
	)
	third := replaceEvidence(
		t,
		index,
		Evidence{URL: "https://x.example", ContentHash: "same", Text: "short"},
	)
	if first.ClusterID != second.ClusterID {
		t.Fatal("first two exact copies did not share a cluster")
	}
	if third.ClusterID == first.ClusterID {
		t.Fatal("full cluster accepted another member")
	}
	for _, url := range []string{"https://z.example", "https://y.example", "https://x.example"} {
		lookupAssignment(t, index, url)
	}
	err := v.View(t.Context(), func(tx *vault.Txn) error {
		posting, found, readErr := index.exactBuckets.Get(tx, vault.Key("same"))
		if readErr != nil {
			return fmt.Errorf("read exact posting: %w", readErr)
		}
		if !found || len(posting.URLs) > limits.MaximumBucketMembers {
			t.Fatalf("bounded posting = %+v, %v", posting, found)
		}
		if !slices.IsSorted(posting.URLs) {
			t.Fatalf("posting is not sorted: %v", posting.URLs)
		}

		return nil
	})
	if err != nil {
		t.Fatalf("inspect exact posting: %v", err)
	}
}

func TestBandPostingFanoutIsBoundedWithoutDroppingURLs(t *testing.T) {
	limits := DefaultLimits()
	limits.MaximumBucketMembers = 2
	limits.ShingleWords = 1
	index, v := openMemoryIndex(t, limits)
	text := "alpha beta gamma delta"
	var clusterID string
	for _, input := range []Evidence{
		{URL: "https://z.example", ContentHash: "z", Text: text},
		{URL: "https://y.example", ContentHash: "y", Text: text},
		{URL: "https://x.example", ContentHash: "x", Text: text},
	} {
		assignment := replaceEvidence(t, index, input)
		if clusterID == "" {
			clusterID = assignment.ClusterID
		}
		if assignment.ClusterID != clusterID {
			t.Fatalf("near cluster = %q, want %q", assignment.ClusterID, clusterID)
		}
	}
	prepared, err := prepareEvidence(t.Context(), limits, Evidence{
		URL:         "https://query.example",
		ContentHash: "query",
		Text:        text,
	})
	if err != nil {
		t.Fatalf("prepare band inspection: %v", err)
	}
	err = v.View(t.Context(), func(tx *vault.Txn) error {
		for band, value := range prepared.Bands {
			posting, found, readErr := index.bandBuckets.Get(
				tx,
				bandKey(uint8(band), value),
			)
			if readErr != nil {
				return fmt.Errorf("read band posting: %w", readErr)
			}
			if !found || len(posting.URLs) != limits.MaximumBucketMembers ||
				!slices.IsSorted(posting.URLs) {
				t.Fatalf("band %d posting = %+v, %v", band, posting, found)
			}
		}

		return nil
	})
	if err != nil {
		t.Fatalf("inspect band postings: %v", err)
	}
	for _, url := range []string{"https://z.example", "https://y.example", "https://x.example"} {
		lookupAssignment(t, index, url)
	}
}

func TestBoltVaultReopenRestoresAllClusterProjections(t *testing.T) {
	path := filepath.Join(t.TempDir(), "content-clusters.db")
	firstVault, err := boltvault.Open(path, 0)
	if err != nil {
		t.Fatalf("open first vault: %v", err)
	}
	firstIndex, err := Open(firstVault, Limits{})
	if err != nil {
		t.Fatalf("open first index: %v", err)
	}
	baseText := "alpha beta gamma delta epsilon zeta eta theta"
	base := replaceEvidence(
		t,
		firstIndex,
		Evidence{URL: "https://a.example", ContentHash: "a", Text: baseText},
	)
	near := replaceEvidence(
		t,
		firstIndex,
		Evidence{
			URL:                "https://b.example",
			ContentHash:        "b",
			Text:               baseText,
			CanonicalPreferred: true,
		},
	)
	if base.ClusterID != near.ClusterID {
		t.Fatal("pre-reopen near copies did not cluster")
	}
	if err := firstVault.Close(); err != nil {
		t.Fatalf("close first vault: %v", err)
	}
	reopenedVault, err := boltvault.Open(path, 0)
	if err != nil {
		t.Fatalf("reopen vault: %v", err)
	}
	t.Cleanup(func() { _ = reopenedVault.Close() })
	reopened, err := Open(reopenedVault, Limits{})
	if err != nil {
		t.Fatalf("open restored index: %v", err)
	}
	if got := lookupAssignment(t, reopened, "https://a.example"); got != near {
		t.Fatalf("restored assignment = %+v, want %+v", got, near)
	}
	exact := replaceEvidence(
		t,
		reopened,
		Evidence{
			URL:         "https://exact.example",
			ContentHash: "a",
			Text:        "entirely unrelated words verify exact projection recovery",
		},
	)
	if exact.ClusterID != base.ClusterID {
		t.Fatalf("restored exact posting cluster = %q, want %q", exact.ClusterID, base.ClusterID)
	}
	third := replaceEvidence(
		t,
		reopened,
		Evidence{URL: "https://c.example", ContentHash: "c", Text: baseText, Quality: 5},
	)
	if third.ClusterID != base.ClusterID {
		t.Fatalf("restored band posting cluster = %q, want %q", third.ClusterID, base.ClusterID)
	}
	if third.RepresentativeURL != "https://b.example" {
		t.Fatalf("restored representative metadata selected %q", third.RepresentativeURL)
	}
}

func TestPublicValidationAndCancellation(t *testing.T) {
	index, _ := openMemoryIndex(t, Limits{})
	invalid := []Evidence{
		{URL: "", ContentHash: "hash"},
		{URL: string(make([]byte, maximumURLBytes+1)), ContentHash: "hash"},
		{URL: "https://a.example", ContentHash: ""},
		{URL: "https://a.example", ContentHash: string(make([]byte, maximumContentHashBytes+1))},
		{URL: "https://a.example", ContentHash: "hash", Quality: math.NaN()},
		{URL: "https://a.example", ContentHash: "hash", Quality: math.Inf(1)},
		{URL: "https://a.example", ContentHash: "hash", InboundAuthority: math.NaN()},
		{URL: "https://a.example", ContentHash: "hash", InboundAuthority: math.Inf(-1)},
	}
	for position, evidence := range invalid {
		if _, err := index.Replace(t.Context(), evidence); !errors.Is(err, ErrInvalidEvidence) {
			t.Fatalf("invalid evidence %d error = %v", position, err)
		}
	}
	if _, _, err := index.Lookup(t.Context(), ""); !errors.Is(err, ErrInvalidEvidence) {
		t.Fatalf("invalid lookup error = %v", err)
	}
	if _, err := index.Delete(t.Context(), ""); !errors.Is(err, ErrInvalidEvidence) {
		t.Fatalf("invalid delete error = %v", err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	valid := Evidence{URL: "https://a.example", ContentHash: "hash", Text: "one two three four"}
	if _, err := index.Replace(cancelled, valid); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled replace error = %v", err)
	}
	if _, err := index.Delete(cancelled, valid.URL); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled delete error = %v", err)
	}
	if _, _, err := index.Lookup(cancelled, valid.URL); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled lookup error = %v", err)
	}
}

func permutations(size int) [][]int {
	values := make([]int, size)
	for position := range values {
		values[position] = position
	}
	var result [][]int
	var visit func(int)
	visit = func(position int) {
		if position == len(values) {
			result = append(result, append([]int(nil), values...))

			return
		}
		for candidate := position; candidate < len(values); candidate++ {
			values[position], values[candidate] = values[candidate], values[position]
			visit(position + 1)
			values[position], values[candidate] = values[candidate], values[position]
		}
	}
	visit(0)

	return result
}
