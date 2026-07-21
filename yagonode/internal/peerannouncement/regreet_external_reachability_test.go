package peerannouncement

import (
	"context"
	"errors"
	"net/netip"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
)

func TestAnnounceRecordsExternalSeniorClassification(t *testing.T) {
	peer := callerSeed(t, "peer", "1.1.1.1")
	evidence := NewExternalReachabilityEvidence()
	announcer := &announcer{
		self:   stubSelf{seed: callerSeed(t, "self", "203.0.113.9")},
		roster: &stubRoster{rounds: [][]yagomodel.Seed{{peer}}},
		greeter: &stubGreeter{result: greetResult{
			YourType: yagomodel.PeerSenior,
		}},
		externalReachabilityEvidence: evidence,
		admitExternalObserverAddress: admitObserverAddresses("1.1.1.1"),
	}

	announcer.Announce(t.Context())

	if !evidence.Reachable(t.Context()) {
		t.Fatal("normal greet did not retain senior back-ping evidence")
	}
}

func TestAnnounceRecordsExternalJuniorInvalidation(t *testing.T) {
	peer := callerSeed(t, "peer", "1.1.1.1")
	evidence := NewExternalReachabilityEvidence()
	evidence.Observe(peer.Hash, yagomodel.PeerSenior)
	announcer := &announcer{
		self:   stubSelf{seed: callerSeed(t, "self", "203.0.113.9")},
		roster: &stubRoster{rounds: [][]yagomodel.Seed{{peer}}},
		greeter: &stubGreeter{result: greetResult{
			YourType: yagomodel.PeerJunior,
		}},
		externalReachabilityEvidence: evidence,
		admitExternalObserverAddress: admitObserverAddresses("1.1.1.1"),
	}

	announcer.Announce(t.Context())

	if evidence.Reachable(t.Context()) {
		t.Fatal("normal greet retained invalidated junior evidence")
	}
}

func TestAnnounceTransportFailurePreservesPriorExternalClassification(t *testing.T) {
	peer := callerSeed(t, "peer", "1.1.1.1")
	evidence := NewExternalReachabilityEvidence()
	evidence.Observe(peer.Hash, yagomodel.PeerSenior)
	announcer := &announcer{
		self:                         stubSelf{seed: callerSeed(t, "self", "203.0.113.9")},
		roster:                       &stubRoster{rounds: [][]yagomodel.Seed{{peer}}},
		greeter:                      &stubGreeter{err: errors.New("unavailable")},
		externalReachabilityEvidence: evidence,
	}

	announcer.Announce(t.Context())

	if !evidence.Reachable(t.Context()) {
		t.Fatal("outbound transport failure erased independent ingress evidence")
	}
}

func TestGreetDiscoveredRecordsExternalPrincipalClassification(t *testing.T) {
	peer := callerSeed(t, "peer", "1.1.1.1")
	evidence := NewExternalReachabilityEvidence()
	announcer := &announcer{
		self:   stubSelf{seed: callerSeed(t, "self", "203.0.113.9")},
		roster: &stubRoster{},
		greeter: &stubGreeter{
			result: greetResult{YourType: yagomodel.PeerPrincipal},
		},
		externalReachabilityEvidence: evidence,
		admitExternalObserverAddress: admitObserverAddresses("1.1.1.1"),
	}

	announcer.GreetDiscovered(t.Context(), peer)

	if !evidence.Reachable(t.Context()) {
		t.Fatal("discovered greet did not retain principal back-ping evidence")
	}
}

func TestLANAndReservedObserversDoNotBecomeExternalEvidence(t *testing.T) {
	for _, address := range []string{"192.168.1.5", "203.0.113.5"} {
		t.Run(address, func(t *testing.T) {
			peer := callerSeed(t, "peer", address)
			evidence := NewExternalReachabilityEvidence()
			roster := &stubRoster{rounds: [][]yagomodel.Seed{{peer}}}
			announcer := &announcer{
				self:   stubSelf{seed: callerSeed(t, "self", "192.168.1.9")},
				roster: roster,
				greeter: &stubGreeter{
					result: greetResult{YourType: yagomodel.PeerSenior},
				},
				externalReachabilityEvidence: evidence,
				admitExternalObserverAddress: admitObserverAddresses("1.1.1.1"),
			}

			announcer.GreetDiscovered(t.Context(), peer)
			roster.rounds = [][]yagomodel.Seed{{peer}}
			announcer.Announce(t.Context())

			if snapshot := evidence.Snapshot(t.Context()); snapshot.Known {
				t.Fatalf("non-public observer produced external evidence: %+v", snapshot)
			}
		})
	}
}

func TestObserverWithoutLiteralAddressDoesNotBecomeExternalEvidence(t *testing.T) {
	peer := callerSeed(t, "peer", "public.example")
	evidence := NewExternalReachabilityEvidence()
	announcer := &announcer{
		externalReachabilityEvidence: evidence,
		admitExternalObserverAddress: admitObserverAddresses("1.1.1.1"),
	}

	announcer.observeExternalReachability(peer, yagomodel.PeerSenior)

	if snapshot := evidence.Snapshot(t.Context()); snapshot.Known {
		t.Fatalf("hostname observer produced external evidence: %+v", snapshot)
	}
}

func TestObserverWithoutAddressDoesNotBecomeExternalEvidence(t *testing.T) {
	evidence := NewExternalReachabilityEvidence()
	announcer := &announcer{
		externalReachabilityEvidence: evidence,
		admitExternalObserverAddress: admitObserverAddresses("1.1.1.1"),
	}

	announcer.observeExternalReachability(
		yagomodel.Seed{Hash: hashFor("peer")},
		yagomodel.PeerSenior,
	)

	if snapshot := evidence.Snapshot(t.Context()); snapshot.Known {
		t.Fatalf("addressless observer produced external evidence: %+v", snapshot)
	}
}

type refreshCycleGreeter struct {
	cancel context.CancelFunc
	called chan int
	calls  int
}

func (g *refreshCycleGreeter) Greet(
	context.Context,
	yagomodel.Seed,
	yagomodel.Seed,
	int,
) (greetResult, error) {
	g.calls++
	g.called <- g.calls
	if g.calls == 2 {
		g.cancel()
	}

	return greetResult{YourType: yagomodel.PeerSenior}, nil
}

func TestRunRefreshesExternalEvidenceBeforeSlowAnnouncementCadence(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	peer := callerSeed(t, "peer", "1.1.1.1")
	announcementTicks := make(chan time.Time)
	refreshTicks := make(chan time.Time, 1)
	greeter := &refreshCycleGreeter{cancel: cancel, called: make(chan int, 2)}
	news := &recordingPeerNews{}
	announcer := &announcer{
		interval:                time.Hour,
		externalRefreshInterval: 10 * time.Minute,
		self:                    stubSelf{seed: callerSeed(t, "self", "203.0.113.9")},
		seeds:                   &stubSeedSource{},
		roster:                  &stubRoster{rounds: [][]yagomodel.Seed{{peer}, {peer}}},
		greeter:                 greeter,
		news:                    news,
		ticks: func(interval time.Duration) (<-chan time.Time, func()) {
			if interval == time.Hour {
				return announcementTicks, func() {}
			}

			return refreshTicks, func() {}
		},
	}
	done := make(chan struct{})
	go func() {
		announcer.Run(ctx)
		close(done)
	}()
	if call := <-greeter.called; call != 1 {
		t.Fatalf("initial greet call = %d", call)
	}
	refreshTicks <- time.Now()
	if call := <-greeter.called; call != 2 {
		t.Fatalf("refresh greet call = %d", call)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		cancel()
		t.Fatal("announcer did not stop after refresh")
	}
	if news.rotations != 1 {
		t.Fatalf("news rotations = %d, want only the announcement rotation", news.rotations)
	}
}

func TestExternalRefreshIsDisabledForFasterAnnouncements(t *testing.T) {
	announcer := &announcer{
		interval:                5 * time.Minute,
		externalRefreshInterval: 10 * time.Minute,
		ticks: func(time.Duration) (<-chan time.Time, func()) {
			t.Fatal("separate refresh ticker was created")

			return nil, nil
		},
	}
	ticks, stop := announcer.externalRefreshTicks()
	stop()
	if ticks != nil {
		t.Fatal("separate refresh ticker is active")
	}
}

func TestNewWiresExternalReachabilityEvidence(t *testing.T) {
	evidence := NewExternalReachabilityEvidence()
	admissionCalled := false
	addressAdmission := func(netip.Addr) error {
		admissionCalled = true

		return nil
	}
	announced := New(
		Config{
			Interval:                     time.Hour,
			ExternalReachabilityEvidence: evidence,
			AdmitExternalObserverAddress: addressAdmission,
		},
		stubSelf{seed: callerSeed(t, "self", "203.0.113.9")},
		&stubSeedSource{},
		&stubRoster{},
	)

	if announced.(*announcer).externalReachabilityEvidence != evidence {
		t.Fatal("New did not retain the supplied external reachability evidence")
	}
	if announced.(*announcer).admitExternalObserverAddress == nil {
		t.Fatal("New did not retain the supplied external observer address admission")
	}
	if err := announced.(*announcer).admitExternalObserverAddress(netip.Addr{}); err != nil {
		t.Fatalf("stored external observer address admission: %v", err)
	}
	if !admissionCalled {
		t.Fatal("stored external observer address admission was not the supplied function")
	}
}
