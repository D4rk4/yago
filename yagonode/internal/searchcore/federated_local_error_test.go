package searchcore

import (
	"context"
	"errors"
	"testing"
)

func TestFederatedSearcherLocalOnlyReturnsLocalError(t *testing.T) {
	response, err := NewFederatedSearcher(
		&fakeCoreSearcher{err: errors.New("local failed")},
		&fakeCoreSearcher{},
	).Search(t.Context(), Request{Source: SourceLocal, Limit: 10})
	if !errors.Is(err, errFederatedSearchUnavailable) ||
		len(response.PartialFailures) != 1 ||
		response.PartialFailures[0].Reason != federatedLocalSearchFailed {
		t.Fatalf("response = %#v, error = %v", response, err)
	}
}

func TestFederatedSearcherLocalOnlyKeepsRowsReturnedWithError(t *testing.T) {
	response, err := NewFederatedSearcher(
		&fakeCoreSearcher{
			response: Response{Results: []Result{{URL: "https://local.example/hit"}}},
			err:      errors.New("local failed"),
		},
		&fakeCoreSearcher{},
	).Search(t.Context(), Request{Source: SourceLocal, Limit: 10})
	if err != nil || len(response.Results) != 1 ||
		len(response.PartialFailures) != 1 ||
		response.PartialFailures[0].Reason != federatedLocalSearchFailed {
		t.Fatalf("response = %#v, error = %v", response, err)
	}
}

func TestFederatedSearcherLocalOnlyPreservesCallerCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	response, err := NewFederatedSearcher(
		&fakeCoreSearcher{err: errors.New("private local failure")},
		&fakeCoreSearcher{},
	).Search(ctx, Request{Source: SourceLocal, Limit: 10})
	if !errors.Is(err, context.Canceled) || len(response.PartialFailures) != 1 {
		t.Fatalf("response = %#v, error = %v", response, err)
	}
}
