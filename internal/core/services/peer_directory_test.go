package services

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

type fakePinger struct {
	err    error
	called bool
}

func (p *fakePinger) Ping(_ context.Context, _ yacymodel.Seed) error {
	p.called = true

	return p.err
}

func callerSeed(hash string, ip string, port int) yacymodel.Seed {
	return yacymodel.Seed{
		yacymodel.SeedHash: string(hashFor(hash)),
		yacymodel.SeedIP:   ip,
		yacymodel.SeedPort: strconv.Itoa(port),
	}
}

func TestHelloClassifiesCaller(t *testing.T) {
	clock := &fakeClock{now: time.Unix(0, 0)}
	cases := []struct {
		name     string
		seed     yacymodel.Seed
		pingErr  error
		want     yacymodel.PeerType
		wantPing bool
	}{
		{"reachable", callerSeed("a", "10.0.0.1", 8090), nil, yacymodel.PeerSenior, true},
		{
			"unreachable",
			callerSeed("a", "10.0.0.1", 8090),
			errors.New("dial failed"),
			yacymodel.PeerJunior,
			true,
		},
		{"no ip", callerSeed("b", "", 8090), nil, yacymodel.PeerJunior, false},
		{"no port", callerSeed("c", "10.0.0.1", 0), nil, yacymodel.PeerJunior, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pinger := &fakePinger{err: tc.pingErr}
			dir := NewPeerDirectory(clock, pinger, 16)
			outcome, err := dir.Hello(context.Background(), tc.seed)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if outcome.CallerType != tc.want {
				t.Errorf("got %v, want %v", outcome.CallerType, tc.want)
			}
			if pinger.called != tc.wantPing {
				t.Errorf("pinger called = %v, want %v", pinger.called, tc.wantPing)
			}
		})
	}
}

func TestHelloStampsLastSeen(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)}
	dir := NewPeerDirectory(clock, &fakePinger{}, 16)

	outcome, err := dir.Hello(context.Background(), callerSeed("a", "10.0.0.1", 8090))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(outcome.Known) != 1 {
		t.Fatalf("got %d known, want 1", len(outcome.Known))
	}
	if got := outcome.Known[0][yacymodel.SeedLastSeen]; got != "2026-06-18T12:00:00" {
		t.Errorf("last seen: got %q", got)
	}
}

func TestHelloBoundedEviction(t *testing.T) {
	clock := &fakeClock{now: time.Unix(0, 0)}
	dir := NewPeerDirectory(clock, &fakePinger{}, 2)

	for _, id := range []string{"a", "b", "c"} {
		if _, err := dir.Hello(context.Background(), callerSeed(id, "10.0.0.1", 8090)); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	outcome, _ := dir.Hello(context.Background(), callerSeed("c", "10.0.0.1", 8090))
	if len(outcome.Known) != 2 {
		t.Fatalf("got %d known, want 2 (bounded)", len(outcome.Known))
	}
	for _, seed := range outcome.Known {
		if seed[yacymodel.SeedHash] == string(hashFor("a")) {
			t.Error("oldest peer should have been evicted")
		}
	}
}
