package yagonode

import (
	"context"
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/peerwire"
)

func (fakeRoster) AnsweredEndpoint(
	context.Context,
	yagomodel.Hash,
) (peerwire.Endpoint, bool, error) {
	return peerwire.Endpoint{}, false, nil
}

func (fakeRoster) ObserveAnsweredCaller(
	context.Context,
	yagomodel.Seed,
	yagomodel.PeerType,
	peerwire.Endpoint,
) {
}

func (fakeRoster) ObserveAnsweredResponder(context.Context, yagomodel.Seed, peerwire.Endpoint) {}

func (r reachableRoster) AnsweredEndpoint(
	context.Context,
	yagomodel.Hash,
) (peerwire.Endpoint, bool, error) {
	return peerwire.Endpoint{}, false, nil
}

func (r reachableRoster) ObserveAnsweredCaller(
	context.Context,
	yagomodel.Seed,
	yagomodel.PeerType,
	peerwire.Endpoint,
) {
}

func (r reachableRoster) ObserveAnsweredResponder(
	context.Context,
	yagomodel.Seed,
	peerwire.Endpoint,
) {
}

func (r *countingPeerRoster) AnsweredEndpoint(
	context.Context,
	yagomodel.Hash,
) (peerwire.Endpoint, bool, error) {
	return peerwire.Endpoint{}, false, nil
}

func (r *countingPeerRoster) ObserveAnsweredCaller(
	ctx context.Context,
	caller yagomodel.Seed,
	classification yagomodel.PeerType,
	_ peerwire.Endpoint,
) {
	r.ObserveCaller(ctx, caller, classification)
}

func (r *countingPeerRoster) ObserveAnsweredResponder(
	ctx context.Context,
	responder yagomodel.Seed,
	_ peerwire.Endpoint,
) {
	r.ObserveResponder(ctx, responder)
}

type answeredEndpointRoster struct {
	reachableRoster
	answered   peerwire.Endpoint
	readErr    error
	remembered map[yagomodel.Hash]peerwire.Endpoint
}

func (r *answeredEndpointRoster) AnsweredEndpoint(
	context.Context,
	yagomodel.Hash,
) (peerwire.Endpoint, bool, error) {
	return r.answered, r.readErr == nil, r.readErr
}

func (r *answeredEndpointRoster) ObserveAnsweredCaller(
	_ context.Context,
	caller yagomodel.Seed,
	_ yagomodel.PeerType,
	endpoint peerwire.Endpoint,
) {
	r.remembered[caller.Hash] = endpoint
}

func TestHelloPeerRosterForwardsAnsweredEndpoints(t *testing.T) {
	peer := yagomodel.Hash("AAAAAAAAAAAA")
	endpoint := peerwire.Endpoint{Scheme: "http", Host: "192.0.2.7", Port: "8090"}
	roster := &answeredEndpointRoster{
		answered:   endpoint,
		remembered: map[yagomodel.Hash]peerwire.Endpoint{},
	}
	adapter := helloPeerRoster{roster: roster}

	got, found, err := adapter.AnsweredEndpoint(t.Context(), peer)
	if err != nil || !found || got != endpoint {
		t.Fatalf("answered endpoint = %+v/%t/%v", got, found, err)
	}
	adapter.ObserveAnsweredCaller(
		t.Context(), yagomodel.Seed{Hash: peer}, yagomodel.PeerSenior, endpoint,
	)
	if roster.remembered[peer] != endpoint {
		t.Fatalf("remembered = %v", roster.remembered)
	}

	roster.readErr = errors.New("roster unavailable")
	if got, found, err := adapter.AnsweredEndpoint(t.Context(), peer); err == nil || found ||
		got != (peerwire.Endpoint{}) {
		t.Fatalf("unreadable answered endpoint = %+v/%t/%v", got, found, err)
	}
}
