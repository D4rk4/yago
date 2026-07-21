package tavilyapi

import (
	"context"
	"errors"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

const (
	extractTimeoutFailureMessage         = "url extraction timed out"
	maximumExtractDocumentLookupDuration = 250 * time.Millisecond
)

func extractDocumentLookupTimedOut(err error) bool {
	return errors.Is(err, context.DeadlineExceeded)
}

func (e extractEndpoint) lookupWithinExtractBudget(
	ctx context.Context,
	normalizedURL string,
) (documentstore.Document, bool, error) {
	lookupContext, cancel := context.WithTimeout(
		ctx,
		extractDocumentLookupDuration(e.documentLookupDuration),
	)
	defer cancel()

	return e.lookup(lookupContext, normalizedURL)
}

func extractDocumentLookupDuration(duration time.Duration) time.Duration {
	if duration <= 0 || duration > maximumExtractDocumentLookupDuration {
		return maximumExtractDocumentLookupDuration
	}

	return duration
}
