package searchremote

import (
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func TestTermAbstractPeerLifecycleUsesTransportOutcomesAndSuccessWins(t *testing.T) {
	peer := serverSeedWithHash(t, "http://127.0.0.1:8090", hashFor("peer"))
	failed := serverSeedWithHash(t, "http://127.0.0.2:8090", hashFor("failed"))
	invalid := serverSeedWithHash(t, "http://127.0.0.3:8090", hashFor("invalid"))
	malformedAbstract := serverSeedWithHash(
		t,
		"http://127.0.0.4:8090",
		hashFor("malformed"),
	)
	sink := &recordingPeerReachability{}
	remote := searcher{lifecycle: newPeerLifecycleSession(sink)}
	remote.observeTermAbstractPeerLifecycle([]peerAbstractOutcome{
		{
			peer:        peer,
			responseErr: errors.Join(errRemoteSearchFailed, errRemoteSearchTransport),
		},
		{peer: peer},
		{
			peer:        failed,
			responseErr: errors.Join(errRemoteSearchFailed, errRemoteSearchTransport),
		},
		{
			peer:        invalid,
			responseErr: errors.Join(errRemoteSearchFailed, errRemoteSearchInvalidResult),
		},
		{peer: malformedAbstract, abstractErr: errors.New("malformed index abstract")},
	})
	remote.lifecycle.flush(t.Context())
	if len(sink.reachable) != 1 || sink.reachable[0] != peer.Hash {
		t.Fatalf("reachable peers = %v", sink.reachable)
	}
	if len(sink.unreachable) != 1 || sink.unreachable[0] != failed.Hash {
		t.Fatalf("unreachable peers = %v", sink.unreachable)
	}
	for _, observed := range append(sink.reachable, sink.unreachable...) {
		if observed == invalid.Hash || observed == malformedAbstract.Hash {
			t.Fatalf("invalid protocol changed reachability for %s", observed)
		}
	}
}

func TestTermAbstractCatalogFeedsPeerLifecycle(t *testing.T) {
	term := hashFor("term")
	server := abstractResponseServer(
		t,
		term,
		yagomodel.EncodeSearchIndexAbstract([]yagomodel.Hash{hashFor("resource")}),
		func() {},
	)
	defer server.Close()
	peer := serverSeedWithHash(t, server.URL, hashFor("peer"))
	sink := &recordingPeerReachability{}
	remote := searcher{
		client:         server.Client(),
		concurrency:    1,
		perPeerTimeout: DefaultPerPeerTimeout,
		lifecycle:      newPeerLifecycleSession(sink),
	}
	budget := newRemoteQueryBudget()
	remote.termAbstractCatalogWithinBudget(
		t.Context(),
		searchcore.Request{Terms: []string{"term"}},
		[]termPeerTargets{{term: term, peers: []yagomodel.Seed{peer}}},
		nil,
		budget,
	)
	remote.lifecycle.flush(t.Context())
	if len(sink.reachable) != 1 || sink.reachable[0] != peer.Hash ||
		len(sink.unreachable) != 0 {
		t.Fatalf("abstract lifecycle = reachable %v unreachable %v",
			sink.reachable, sink.unreachable)
	}
}
