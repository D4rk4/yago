package yagonode

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func openBoundConditionalNode(t *testing.T) *conditionalRecrawlNode {
	t.Helper()
	node := openConditionalRecrawlNode(t, filepath.Join(t.TempDir(), "node.db"), time.Hour)
	if outcome := node.submit(
		t, fullConditionalPage(t, conditionalBase, "first text", firstValidators()),
	); outcome != "ack" {
		t.Fatalf("full page settlement = %q, want ack", outcome)
	}

	return node
}

func firstRevision() yagocrawlcontract.DocumentRevision {
	return yagocrawlcontract.DocumentRevision{
		ContentHash:          "sha256:first text",
		ExtractionGeneration: yagocrawlcontract.CurrentExtractionGeneration,
		FetchedAt:            conditionalBase,
	}
}

func TestConditionalRecrawlRefusesObservationsThatNoLongerMatchTheStoredPage(t *testing.T) {
	observedAt := conditionalBase.Add(2 * time.Hour)
	for _, tc := range []struct {
		name     string
		before   func(*testing.T, *conditionalRecrawlNode)
		revision func() yagocrawlcontract.DocumentRevision
		want     string
	}{
		{name: "matching", before: func(*testing.T, *conditionalRecrawlNode) {}, want: "recorded"},
		{
			name: "changed page",
			before: func(t *testing.T, node *conditionalRecrawlNode) {
				node.submit(t, fullConditionalPage(
					t, conditionalBase.Add(time.Hour), "second text",
					&yagocrawlcontract.HTTPValidators{ETag: `"second"`},
				))
			},
		},
		{
			name: "removed page",
			before: func(t *testing.T, node *conditionalRecrawlNode) {
				node.submit(t, yagocrawlcontract.IngestBatch{
					SourceURL:     conditionalPageURL,
					ObservationID: "removed",
					ObservedAt:    conditionalBase.Add(time.Hour),
					Removed:       true,
				})
			},
		},
		{
			name: "newer observation recorded",
			before: func(t *testing.T, node *conditionalRecrawlNode) {
				node.submit(t, notModifiedConditionalPage(
					firstRevision(), observedAt.Add(time.Minute), "newer",
				))
			},
		},
		{
			name:   "foreign revision",
			before: func(*testing.T, *conditionalRecrawlNode) {},
			revision: func() yagocrawlcontract.DocumentRevision {
				revision := firstRevision()
				revision.FetchedAt = revision.FetchedAt.Add(time.Second)

				return revision
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			node := openBoundConditionalNode(t)
			tc.before(t, node)
			revision := firstRevision()
			if tc.revision != nil {
				revision = tc.revision()
			}
			want := tc.want
			if want == "" {
				want = "ack"
			}

			outcome := node.submit(t, notModifiedConditionalPage(revision, observedAt, "observed"))

			if outcome != want {
				t.Fatalf("not-modified settlement = %q, want %q", outcome, want)
			}
		})
	}
}

func TestConditionalRecrawlIssuesValidatorsOnlyForTheCurrentStoredRevision(t *testing.T) {
	for _, tc := range []struct {
		name       string
		generation uint64
		validators *yagocrawlcontract.HTTPValidators
		issued     bool
	}{
		{
			name:       "current generation",
			generation: yagocrawlcontract.CurrentExtractionGeneration,
			validators: firstValidators(),
			issued:     true,
		},
		{
			name:       "newer generation",
			generation: yagocrawlcontract.CurrentExtractionGeneration + 1,
			validators: firstValidators(),
		},
		{name: "legacy generation", validators: firstValidators()},
		{name: "no validators", generation: yagocrawlcontract.CurrentExtractionGeneration},
		{
			name:       "malformed validators",
			generation: yagocrawlcontract.CurrentExtractionGeneration,
			validators: &yagocrawlcontract.HTTPValidators{ETag: "unquoted"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			node := openConditionalRecrawlNode(
				t, filepath.Join(t.TempDir(), "node.db"), time.Hour,
			)
			page := fullConditionalPage(t, conditionalBase, "first text", tc.validators)
			page.Document.ExtractionGeneration = tc.generation
			if outcome := node.submit(t, page); outcome != "ack" {
				t.Fatalf("full page settlement = %q, want ack", outcome)
			}
			revision := firstRevision()
			revision.ExtractionGeneration = tc.generation

			fetch := soleConditionalFetch(t, node.dueRequests(t, conditionalBase.Add(time.Hour)))
			outcome := node.submit(t, notModifiedConditionalPage(
				revision, conditionalBase.Add(time.Hour), "observed",
			))

			if (fetch != nil) != tc.issued || (outcome == "recorded") != tc.issued {
				t.Fatalf("issued %+v and settled %q, want issued %v", fetch, outcome, tc.issued)
			}
		})
	}
}

func TestConditionalRecrawlBindsTheStoredRevisionOfAnUnchangedRefetch(t *testing.T) {
	node := openBoundConditionalNode(t)
	second := &yagocrawlcontract.HTTPValidators{ETag: `"second"`}

	if outcome := node.submit(t, fullConditionalPage(
		t, conditionalBase.Add(time.Hour), "first text", second,
	)); outcome != "ack" {
		t.Fatalf("unchanged refetch settlement = %q, want ack", outcome)
	}

	fetch := soleConditionalFetch(t, node.dueRequests(t, conditionalBase.Add(2*time.Hour)))
	if fetch == nil || fetch.Validators != *second || fetch.Revision != firstRevision() {
		t.Fatalf("conditional fetch = %+v, want the new validators on the stored revision", fetch)
	}
}

func TestConditionalRecrawlKeepsValidatorsOfARefusedIngest(t *testing.T) {
	node := openBoundConditionalNode(t)
	refused := fullConditionalPage(
		t, conditionalBase.Add(time.Minute), "refused text",
		&yagocrawlcontract.HTTPValidators{ETag: `"refused"`},
	)
	refused.Document.NormalizedURL = ""
	refused.Document.CanonicalURL = ""

	if outcome := node.submit(t, refused); outcome == "recorded" {
		t.Fatalf("refused ingest settlement = %q", outcome)
	}

	fetch := soleConditionalFetch(t, node.dueRequests(t, conditionalBase.Add(time.Hour)))
	if fetch == nil || fetch.Validators != *firstValidators() {
		t.Fatalf("conditional fetch after refused ingest = %+v, want the first validators", fetch)
	}
}

func TestConditionalRecrawlNeverSchedulesANeverRecrawlProfile(t *testing.T) {
	node := openConditionalRecrawlNode(t, filepath.Join(t.TempDir(), "node.db"), 0)
	if outcome := node.submit(
		t, fullConditionalPage(t, conditionalBase, "first text", firstValidators()),
	); outcome != "ack" {
		t.Fatalf("full page settlement = %q, want ack", outcome)
	}

	if requests := node.dueRequests(t, conditionalBase.Add(24*time.Hour)); len(requests) != 0 {
		t.Fatalf("never-recrawl profile dispatched %+v", requests)
	}
	if outcome := node.submit(t, notModifiedConditionalPage(
		firstRevision(), conditionalBase.Add(time.Hour), "observed",
	)); outcome != "ack" {
		t.Fatalf("not-modified for a never-recrawl page settled %q, want ack", outcome)
	}
}

func TestConditionalRecrawlFollowsAFetchedURLToItsStoredDocument(t *testing.T) {
	for _, tc := range []struct {
		name          string
		normalizedURL string
		canonicalURL  string
		storedURL     string
	}{
		{
			name:          "normalized document URL",
			normalizedURL: conditionalPageURL + "-final",
			canonicalURL:  conditionalPageURL + "-canonical",
			storedURL:     conditionalPageURL + "-final",
		},
		{
			name:         "canonical document URL",
			canonicalURL: conditionalPageURL + "-canonical",
			storedURL:    conditionalPageURL + "-canonical",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			node := openConditionalRecrawlNode(
				t, filepath.Join(t.TempDir(), "node.db"), time.Hour,
			)
			page := fullConditionalPage(t, conditionalBase, "first text", firstValidators())
			page.Document.NormalizedURL = tc.normalizedURL
			page.Document.CanonicalURL = tc.canonicalURL
			if outcome := node.submit(t, page); outcome != "ack" {
				t.Fatalf("full page settlement = %q, want ack", outcome)
			}

			binding, bound, err := node.ingest.frontier.ValidatorBinding(ctx, conditionalPageURL)
			if err != nil || !bound || binding.DocumentURL != tc.storedURL {
				t.Fatalf(
					"binding = %+v, %v, %v, want document %q",
					binding,
					bound,
					err,
					tc.storedURL,
				)
			}
			fetch := node.ingest.conditional.issuedConditionalFetch(ctx, conditionalPageURL)
			if fetch == nil || fetch.Validators != *firstValidators() {
				t.Fatalf("issued conditional fetch = %+v, want the first validators", fetch)
			}
			if outcome := node.submit(t, notModifiedConditionalPage(
				fetch.Revision, conditionalBase.Add(time.Hour), "unchanged",
			)); outcome != "recorded" {
				t.Fatalf("not-modified settlement = %q, want recorded", outcome)
			}
		})
	}
}
