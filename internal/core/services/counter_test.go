package services

import (
	"context"
	"errors"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/internal/core/contracts"
)

func TestCounterCount(t *testing.T) {
	rwi := &fakeRWIStore{rwiCount: 3, referencedURLs: 5}
	urls := &fakeURLStore{urlCount: 7}
	counter := NewCounter(rwi, urls)

	cases := []struct {
		kind contracts.CountKind
		want int
	}{
		{contracts.RWICount, 3},
		{contracts.RWIURLCount, 5},
		{contracts.LURLCount, 7},
	}
	for _, tc := range cases {
		got, err := counter.Count(context.Background(), tc.kind)
		if err != nil {
			t.Fatalf("kind %d: unexpected error: %v", tc.kind, err)
		}
		if got != tc.want {
			t.Errorf("kind %d: got %d, want %d", tc.kind, got, tc.want)
		}
	}
}

func TestCounterUnknownKind(t *testing.T) {
	counter := NewCounter(&fakeRWIStore{}, &fakeURLStore{})

	if _, err := counter.Count(
		context.Background(),
		contracts.CountKind(99),
	); !errors.Is(
		err,
		ErrUnknownCountKind,
	) {
		t.Fatalf("got %v, want ErrUnknownCountKind", err)
	}
}
