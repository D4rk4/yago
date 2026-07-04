package searchcore

import (
	"errors"
	"testing"
)

func TestFederatedSearcherLocalOnlyReturnsLocalError(t *testing.T) {
	_, err := NewFederatedSearcher(
		&fakeCoreSearcher{err: errors.New("local failed")},
		&fakeCoreSearcher{},
	).Search(t.Context(), Request{Source: SourceLocal, Limit: 10})
	if err == nil {
		t.Fatal("expected the local error to surface for a local-only request")
	}
}
