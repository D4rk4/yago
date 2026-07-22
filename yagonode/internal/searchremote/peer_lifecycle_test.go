package searchremote

import (
	"context"
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
)

type recordingPeerReachability struct {
	reachable   []yagomodel.Hash
	unreachable []yagomodel.Hash
}

func (r *recordingPeerReachability) ConfirmReachable(_ context.Context, peer yagomodel.Hash) {
	r.reachable = append(r.reachable, peer)
}

func (r *recordingPeerReachability) ConfirmUnreachable(_ context.Context, peer yagomodel.Hash) {
	r.unreachable = append(r.unreachable, peer)
}

func TestPeerLifecycleSuccessWinsAndInvalidProtocolDoesNotChangeReachability(t *testing.T) {
	peer := serverSeedWithHash(t, "http://127.0.0.1:8090", hashFor("peer"))
	sink := &recordingPeerReachability{}
	session := newPeerLifecycleSession(sink)
	session.observe([]peerSearchResult{
		{err: errors.Join(errRemoteSearchFailed, errRemoteSearchTransport)},
		{peer: peer, err: errors.Join(errRemoteSearchFailed, errRemoteSearchTransport)},
		{peer: peer, err: errors.Join(errRemoteSearchFailed, errRemoteSearchInvalidResult)},
		{peer: peer},
	})
	session.mu.Lock()
	outcomes := session.outcomes
	session.outcomes = map[yagomodel.Hash]peerLifecycleOutcome{}
	session.mu.Unlock()
	applyPeerLifecycleBatch(t.Context(), peerLifecycleBatch{sink: sink, outcomes: outcomes})
	if len(sink.reachable) != 1 || sink.reachable[0] != peer.Hash ||
		len(sink.unreachable) != 0 {
		t.Fatalf("lifecycle = reachable %v unreachable %v", sink.reachable, sink.unreachable)
	}

	invalidOnly := newPeerLifecycleSession(sink)
	invalidOnly.observe([]peerSearchResult{{
		peer: peer,
		err:  errors.Join(errRemoteSearchFailed, errRemoteSearchInvalidResult),
	}, {
		peer:               peer,
		responseIncomplete: true,
	}})
	if len(invalidOnly.outcomes) != 0 {
		t.Fatalf("invalid protocol changed lifecycle: %v", invalidOnly.outcomes)
	}
}
