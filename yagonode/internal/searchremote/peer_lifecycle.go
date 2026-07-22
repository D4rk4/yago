package searchremote

import (
	"context"
	"errors"
	"sort"
	"sync"

	"github.com/D4rk4/yago/yagomodel"
)

type peerLifecycleOutcome uint8

const (
	peerLifecycleNoChange peerLifecycleOutcome = iota
	peerLifecycleTransportFailure
	peerLifecycleSuccess
)

type peerLifecycleBatch struct {
	sink     PeerReachability
	outcomes map[yagomodel.Hash]peerLifecycleOutcome
}

type peerLifecycleSession struct {
	sink     PeerReachability
	mu       sync.Mutex
	outcomes map[yagomodel.Hash]peerLifecycleOutcome
}

func newPeerLifecycleSession(sink PeerReachability) *peerLifecycleSession {
	if sink == nil {
		return nil
	}

	return &peerLifecycleSession{
		sink:     sink,
		outcomes: make(map[yagomodel.Hash]peerLifecycleOutcome),
	}
}

func (s *peerLifecycleSession) observe(results []peerSearchResult) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, result := range results {
		if result.peer.Hash == "" {
			continue
		}
		outcome := peerLifecycleNoChange
		switch {
		case result.err == nil && !result.responseIncomplete:
			outcome = peerLifecycleSuccess
		case errors.Is(result.err, errRemoteSearchTransport):
			outcome = peerLifecycleTransportFailure
		}
		if outcome > s.outcomes[result.peer.Hash] {
			s.outcomes[result.peer.Hash] = outcome
		}
	}
}

func (s *peerLifecycleSession) flush(ctx context.Context) {
	if s == nil {
		return
	}
	s.mu.Lock()
	outcomes := s.outcomes
	s.outcomes = make(map[yagomodel.Hash]peerLifecycleOutcome)
	s.mu.Unlock()
	applyPeerLifecycleBatch(ctx, peerLifecycleBatch{sink: s.sink, outcomes: outcomes})
}

func applyPeerLifecycleBatch(ctx context.Context, batch peerLifecycleBatch) {
	peers := make([]yagomodel.Hash, 0, len(batch.outcomes))
	for peer := range batch.outcomes {
		peers = append(peers, peer)
	}
	sort.Slice(peers, func(left, right int) bool {
		return peers[left].String() < peers[right].String()
	})
	for _, peer := range peers {
		switch batch.outcomes[peer] {
		case peerLifecycleSuccess:
			batch.sink.ConfirmReachable(ctx, peer)
		case peerLifecycleTransportFailure:
			batch.sink.ConfirmUnreachable(ctx, peer)
		}
	}
}
