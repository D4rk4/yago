package yagonode

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

type portalProgressObserverProbe struct {
	observed bool
}

func (probe *portalProgressObserverProbe) Search(
	ctx context.Context,
	_ searchcore.Request,
) (searchcore.Response, error) {
	observer := searchcore.SearchProgressObserverFrom(ctx)
	probe.observed = observer != nil
	if observer != nil {
		observer(searchcore.SearchProgress{})
	}

	return searchcore.Response{}, nil
}

func TestPortalSearchEnablesProgressiveLocalRetrieval(t *testing.T) {
	probe := &portalProgressObserverProbe{}
	if _, err := newPortalSource(probe).Search(
		t.Context(), "sample query", "", 0, 10,
	); err != nil {
		t.Fatalf("portal search: %v", err)
	}
	if !probe.observed {
		t.Fatal("portal search did not enable progressive local retrieval")
	}
}

func TestPortalSearchPreservesExistingProgressObserver(t *testing.T) {
	probe := &portalProgressObserverProbe{}
	observed := false
	ctx := searchcore.WithSearchProgressObserver(t.Context(), func(searchcore.SearchProgress) {
		observed = true
	})
	if _, err := newPortalSource(probe).Search(ctx, "sample query", "", 0, 10); err != nil {
		t.Fatalf("portal search: %v", err)
	}
	if !probe.observed || !observed {
		t.Fatal("portal search replaced or skipped the existing progress observer")
	}
}
