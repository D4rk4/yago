package yagonode

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/recrawlfrontier"
)

const msgConditionalFetchLookupFailed = "conditional recrawl lookup failed"

type conditionalRecrawl struct {
	frontier  *recrawlfrontier.Frontier
	documents documentstore.DocumentDirectory
}

func acceptedDocumentURL(document yagocrawlcontract.DocumentIngest) string {
	if document.NormalizedURL != "" {
		return document.NormalizedURL
	}

	return document.CanonicalURL
}

func storedDocumentRevision(document documentstore.Document) yagocrawlcontract.DocumentRevision {
	return yagocrawlcontract.DocumentRevision{
		ContentHash:          document.ContentHash,
		ExtractionGeneration: document.ExtractionGeneration,
		FetchedAt:            document.FetchedAt,
	}
}

func (c conditionalRecrawl) RecordResponseValidators(
	ctx context.Context,
	batch yagocrawlcontract.IngestBatch,
) error {
	if batch.ResponseValidators == nil {
		return nil
	}
	documentURL := acceptedDocumentURL(batch.Document)
	stored, found, err := c.documents.Document(ctx, documentURL)
	if err != nil {
		return fmt.Errorf("read accepted document: %w", err)
	}
	if !found || stored.ContentHash != batch.Document.ContentHash ||
		stored.ExtractionGeneration != batch.Document.ExtractionGeneration {
		return nil
	}
	if err := c.frontier.BindHTTPValidators(ctx, batch.SourceURL, recrawlfrontier.ValidatorBinding{
		DocumentURL: documentURL,
		Validators:  *batch.ResponseValidators,
		Revision:    storedDocumentRevision(stored),
		ObservedAt:  batch.ObservedAt,
	}); err != nil {
		return fmt.Errorf("record response validators: %w", err)
	}

	return nil
}

func (c conditionalRecrawl) RecordNotModifiedObservation(
	ctx context.Context,
	batch yagocrawlcontract.IngestBatch,
) (bool, error) {
	_, current, err := c.currentBinding(ctx, batch.SourceURL)
	if err != nil || !current {
		return false, err
	}
	recorded, err := c.frontier.RecordUnchangedPage(ctx, recrawlfrontier.UnchangedPage{
		URL:              batch.SourceURL,
		ProfileHandle:    batch.ProfileHandle,
		Revision:         batch.NotModified.Revision,
		Validators:       batch.ResponseValidators,
		ObservedAt:       batch.ObservedAt,
		SourceModifiedAt: batch.SourceModifiedAt,
	})
	if err != nil {
		return false, fmt.Errorf("record not-modified observation: %w", err)
	}

	return recorded, nil
}

func (c conditionalRecrawl) issuedConditionalFetch(
	ctx context.Context,
	url string,
) *yagocrawlcontract.ConditionalFetch {
	fetch, err := c.conditionalFetch(ctx, url)
	if err != nil {
		slog.WarnContext(ctx, msgConditionalFetchLookupFailed,
			slog.String("url", url), slog.Any("error", err))
	}

	return fetch
}

func (c conditionalRecrawl) conditionalFetch(
	ctx context.Context,
	url string,
) (*yagocrawlcontract.ConditionalFetch, error) {
	binding, current, err := c.currentBinding(ctx, url)
	if err != nil || !current {
		return nil, err
	}
	return &yagocrawlcontract.ConditionalFetch{
		Validators: binding.Validators,
		Revision:   binding.Revision,
	}, nil
}

func (c conditionalRecrawl) currentBinding(
	ctx context.Context,
	url string,
) (recrawlfrontier.ValidatorBinding, bool, error) {
	binding, found, err := c.frontier.ValidatorBinding(ctx, url)
	if err != nil {
		return recrawlfrontier.ValidatorBinding{}, false, fmt.Errorf(
			"read validator binding: %w",
			err,
		)
	}
	if !found {
		return recrawlfrontier.ValidatorBinding{}, false, nil
	}
	document, present, err := c.documents.Document(ctx, binding.DocumentURL)
	if err != nil {
		return recrawlfrontier.ValidatorBinding{}, false, fmt.Errorf(
			"read stored document: %w",
			err,
		)
	}
	current := present &&
		document.ExtractionGeneration == yagocrawlcontract.CurrentExtractionGeneration &&
		binding.Revision.Matches(storedDocumentRevision(document))

	return binding, current, nil
}
