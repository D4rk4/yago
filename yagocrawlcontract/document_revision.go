package yagocrawlcontract

import (
	"errors"
	"fmt"
	"time"
)

var (
	errDocumentRevisionIncomplete = errors.New("document revision is incomplete")
	errDocumentRevisionTooLarge   = errors.New("document revision content hash exceeds size limit")
)

type DocumentRevision struct {
	ContentHash          string
	ExtractionGeneration uint64
	FetchedAt            time.Time
}

func (r DocumentRevision) Validate() error {
	if r.ContentHash == "" || r.ExtractionGeneration == 0 || r.FetchedAt.IsZero() {
		return errDocumentRevisionIncomplete
	}
	if len(r.ContentHash) > MaximumDocumentMetadataBytes {
		return fmt.Errorf("%w: %d bytes", errDocumentRevisionTooLarge, len(r.ContentHash))
	}

	return nil
}

func (r DocumentRevision) Matches(current DocumentRevision) bool {
	return r.ContentHash == current.ContentHash &&
		r.ExtractionGeneration == current.ExtractionGeneration &&
		r.FetchedAt.Equal(current.FetchedAt)
}
