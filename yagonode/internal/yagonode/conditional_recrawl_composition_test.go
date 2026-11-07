package yagonode

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/memvault"
	"github.com/D4rk4/yago/yagonode/internal/recrawlfrontier"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

var errStoredDocumentUnreadable = errors.New("stored document unreadable")

type scriptedDocumentDirectory struct {
	document documentstore.Document
	found    bool
	err      error
	before   func()
}

func (d scriptedDocumentDirectory) Document(
	context.Context,
	string,
) (documentstore.Document, bool, error) {
	if d.before != nil {
		d.before()
	}

	return d.document, d.found, d.err
}

func (scriptedDocumentDirectory) Count(context.Context) (int, error) { return 0, nil }

func storedConditionalDocument() documentstore.Document {
	return documentstore.Document{
		NormalizedURL:        conditionalPageURL,
		ContentHash:          "sha256:first text",
		ExtractionGeneration: yagocrawlcontract.CurrentExtractionGeneration,
		FetchedAt:            conditionalBase,
	}
}

func scheduledConditionalFrontier(t *testing.T) (*recrawlfrontier.Frontier, *vault.Vault) {
	t.Helper()
	storage, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("open recrawl storage: %v", err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	frontier, err := recrawlfrontier.Open(storage)
	if err != nil {
		t.Fatalf("open recrawl frontier: %v", err)
	}
	if err := frontier.Observe(
		context.Background(), conditionalPageURL, "handle-a", time.Hour, conditionalBase,
	); err != nil {
		t.Fatalf("observe page: %v", err)
	}

	return frontier, storage
}

func validatedConditionalBatch() yagocrawlcontract.IngestBatch {
	return yagocrawlcontract.IngestBatch{
		SourceURL:          conditionalPageURL,
		ProfileHandle:      "handle-a",
		ObservedAt:         conditionalBase,
		ResponseValidators: firstValidators(),
		NotModified:        &yagocrawlcontract.NotModifiedObservation{Revision: firstRevision()},
		Document: yagocrawlcontract.DocumentIngest{
			NormalizedURL:        conditionalPageURL,
			ContentHash:          "sha256:first text",
			ExtractionGeneration: yagocrawlcontract.CurrentExtractionGeneration,
		},
	}
}

func boundConditionalRecrawl(
	t *testing.T,
	documents scriptedDocumentDirectory,
) (conditionalRecrawl, *vault.Vault) {
	t.Helper()
	frontier, storage := scheduledConditionalFrontier(t)
	if err := frontier.BindHTTPValidators(
		context.Background(), conditionalPageURL, recrawlfrontier.ValidatorBinding{
			DocumentURL: conditionalPageURL,
			Validators:  *firstValidators(),
			Revision:    firstRevision(),
			ObservedAt:  conditionalBase,
		},
	); err != nil {
		t.Fatalf("bind validators: %v", err)
	}

	return conditionalRecrawl{frontier: frontier, documents: documents}, storage
}

func TestResponseValidatorsBindOnlyToTheDocumentTheyDescribe(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*documentstore.Document)
		found  bool
		bound  bool
	}{
		{name: "same document", change: func(*documentstore.Document) {}, found: true, bound: true},
		{name: "missing document", change: func(*documentstore.Document) {}},
		{
			name:   "other content",
			change: func(d *documentstore.Document) { d.ContentHash = "sha256:other" },
			found:  true,
		},
		{
			name:   "other extraction",
			change: func(d *documentstore.Document) { d.ExtractionGeneration++ },
			found:  true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			frontier, _ := scheduledConditionalFrontier(t)
			document := storedConditionalDocument()
			tc.change(&document)
			conditional := conditionalRecrawl{
				frontier:  frontier,
				documents: scriptedDocumentDirectory{document: document, found: tc.found},
			}

			err := conditional.RecordResponseValidators(
				context.Background(), validatedConditionalBatch(),
			)
			if err != nil {
				t.Fatalf("record response validators: %v", err)
			}

			_, bound, err := frontier.ValidatorBinding(context.Background(), conditionalPageURL)
			if err != nil || bound != tc.bound {
				t.Fatalf("validators bound = %v, %v; want %v", bound, err, tc.bound)
			}
		})
	}
}

func TestConditionalRecrawlFollowsOnlyTheStoredDocumentItWasKeptFor(t *testing.T) {
	for _, tc := range []struct {
		name    string
		change  func(*documentstore.Document)
		present bool
		current bool
	}{
		{name: "same document", change: func(*documentstore.Document) {}, present: true, current: true},
		{name: "removed document", change: func(*documentstore.Document) {}},
		{
			name:    "other content",
			change:  func(d *documentstore.Document) { d.ContentHash = "sha256:other" },
			present: true,
		},
		{
			name:    "other fetch time",
			change:  func(d *documentstore.Document) { d.FetchedAt = d.FetchedAt.Add(time.Second) },
			present: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			document := storedConditionalDocument()
			tc.change(&document)
			conditional, _ := boundConditionalRecrawl(t, scriptedDocumentDirectory{
				document: document, found: tc.present,
			})
			ctx := context.Background()

			fetch := conditional.issuedConditionalFetch(ctx, conditionalPageURL)
			recorded, err := conditional.RecordNotModifiedObservation(
				ctx, validatedConditionalBatch(),
			)

			if err != nil || (fetch != nil) != tc.current || recorded != tc.current {
				t.Fatalf("issued %+v and recorded %v, %v; want current %v",
					fetch, recorded, err, tc.current)
			}
		})
	}
}

func TestConditionalRecrawlReportsUnreadableStoredDocuments(t *testing.T) {
	conditional, _ := boundConditionalRecrawl(
		t, scriptedDocumentDirectory{err: errStoredDocumentUnreadable},
	)
	ctx := context.Background()

	if err := conditional.RecordResponseValidators(
		ctx, validatedConditionalBatch(),
	); !errors.Is(err, errStoredDocumentUnreadable) {
		t.Fatalf("record validators error = %v, want the document error", err)
	}
	recorded, err := conditional.RecordNotModifiedObservation(ctx, validatedConditionalBatch())
	if recorded || !errors.Is(err, errStoredDocumentUnreadable) {
		t.Fatalf("record not-modified = %v, %v; want the document error", recorded, err)
	}
	if fetch := conditional.issuedConditionalFetch(ctx, conditionalPageURL); fetch != nil {
		t.Fatalf("issued %+v without a readable stored document", fetch)
	}
}

func TestConditionalRecrawlReportsUnavailableSchedules(t *testing.T) {
	ctx := context.Background()
	conditional, storage := boundConditionalRecrawl(t, scriptedDocumentDirectory{
		document: storedConditionalDocument(), found: true,
	})
	if fetch := conditional.issuedConditionalFetch(ctx, conditionalPageURL); fetch == nil {
		t.Fatal("bound current page issued no conditional fetch")
	}
	if err := storage.Close(); err != nil {
		t.Fatalf("close recrawl storage: %v", err)
	}

	if fetch := conditional.issuedConditionalFetch(ctx, conditionalPageURL); fetch != nil {
		t.Fatalf("issued %+v from an unavailable schedule", fetch)
	}
	recorded, err := conditional.RecordNotModifiedObservation(ctx, validatedConditionalBatch())
	if recorded || err == nil {
		t.Fatalf("record not-modified = %v, %v; want a schedule error", recorded, err)
	}
}

func TestConditionalRecrawlReportsScheduleWritesThatFail(t *testing.T) {
	ctx := context.Background()
	var storage *vault.Vault
	closeSchedule := func() { _ = storage.Close() }
	conditional, opened := boundConditionalRecrawl(t, scriptedDocumentDirectory{
		document: storedConditionalDocument(), found: true, before: closeSchedule,
	})
	storage = opened

	recorded, err := conditional.RecordNotModifiedObservation(ctx, validatedConditionalBatch())
	if recorded || err == nil {
		t.Fatalf("record not-modified = %v, %v; want a schedule write error", recorded, err)
	}
	if err := conditional.RecordResponseValidators(ctx, validatedConditionalBatch()); err == nil {
		t.Fatal("record validators succeeded with an unavailable schedule")
	}
}
