package crawlresults

import (
	"context"
	"errors"
	"testing"
)

func TestAuthorizeIngestDeliveryRejectsLeaseLossBeforeMutation(t *testing.T) {
	want := errors.New("lease lost")
	leaseLost := 0
	nacked := 0
	released := 0
	delivery := IngestDelivery{
		BeginMutation: func(context.Context) (func(), error) {
			return func() { released++ }, want
		},
		LeaseLost: func(context.Context) error {
			leaseLost++

			return nil
		},
		Nak: func(context.Context) error {
			nacked++

			return nil
		},
	}
	release, authorized := authorizeIngestDelivery(t.Context(), delivery)
	release()
	if authorized || leaseLost != 1 || nacked != 0 || released != 0 {
		t.Fatalf(
			"authorization = %v leaseLost=%d nacked=%d released=%d",
			authorized,
			leaseLost,
			nacked,
			released,
		)
	}
}

func TestAuthorizeIngestGroupRetainsOnlyLiveLeases(t *testing.T) {
	released := 0
	lost := 0
	group := []IngestDelivery{
		{
			BeginMutation: func(context.Context) (func(), error) {
				return func() { released++ }, nil
			},
		},
		{
			BeginMutation: func(context.Context) (func(), error) {
				return nil, errors.New("stale")
			},
			LeaseLost: func(context.Context) error {
				lost++

				return nil
			},
		},
	}
	authorized, releases := authorizeIngestGroup(t.Context(), group)
	if len(authorized) != 1 || len(releases) != 1 || lost != 1 || released != 0 {
		t.Fatalf(
			"authorized=%d releases=%d lost=%d released=%d",
			len(authorized),
			len(releases),
			lost,
			released,
		)
	}
	releaseIngestGroup(releases)
	if released != 1 {
		t.Fatalf("authorized mutation releases = %d, want 1", released)
	}
}

func TestAuthorizeIngestGroupHoldsOneSharedMutationFence(t *testing.T) {
	type groupKey struct{}
	events := make([]string, 0, 7)
	delivery := func(name string) IngestDelivery {
		return IngestDelivery{
			ValidateMutation: func(context.Context) error {
				events = append(events, "validate-"+name)

				return nil
			},
			BeginMutation: func(ctx context.Context) (func(), error) {
				if active, _ := ctx.Value(groupKey{}).(bool); !active {
					t.Fatalf("mutation %s ran outside the group fence", name)
				}
				events = append(events, "authorize-"+name)

				return func() { events = append(events, "release-"+name) }, nil
			},
		}
	}
	first := delivery("first")
	first.BeginMutationGroup = func(ctx context.Context) (context.Context, func()) {
		events = append(events, "begin-group")

		return context.WithValue(ctx, groupKey{}, true), func() {
			events = append(events, "release-group")
		}
	}
	second := delivery("second")
	authorized, releases := authorizeIngestGroup(t.Context(), []IngestDelivery{first, second})
	if len(authorized) != 2 || len(releases) != 3 {
		t.Fatalf("authorized/releases = %d/%d, want 2/3", len(authorized), len(releases))
	}
	releaseIngestGroup(releases)
	want := []string{
		"validate-first", "validate-second", "begin-group",
		"authorize-first", "authorize-second", "release-second",
		"release-first", "release-group",
	}
	if len(events) != len(want) {
		t.Fatalf("events = %v, want %v", events, want)
	}
	for index := range want {
		if events[index] != want[index] {
			t.Fatalf("events = %v, want %v", events, want)
		}
	}
}

func TestAuthorizeIngestGroupValidatesBeforeOpeningMutationFence(t *testing.T) {
	leaseLost := 0
	groupOpened := false
	stale := IngestDelivery{
		ValidateMutation: func(context.Context) error { return errors.New("stale") },
		LeaseLost: func(context.Context) error {
			leaseLost++

			return nil
		},
	}
	live := IngestDelivery{
		BeginMutationGroup: func(ctx context.Context) (context.Context, func()) {
			groupOpened = true

			return ctx, func() {}
		},
		BeginMutation: func(context.Context) (func(), error) {
			return func() {}, nil
		},
	}
	authorized, releases := authorizeIngestGroup(t.Context(), []IngestDelivery{stale, live})
	if len(authorized) != 1 || len(releases) != 1 || leaseLost != 1 || groupOpened {
		t.Fatalf(
			"authorized/releases/lost/group = %d/%d/%d/%t",
			len(authorized), len(releases), leaseLost, groupOpened,
		)
	}
	releaseIngestGroup(releases)
}
