package yagonode

import (
	"context"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/peerannouncement"
)

type externalReachabilitySnapshots interface {
	Snapshot(context.Context) peerannouncement.ExternalReachabilitySnapshot
}

type peerBackPingPublicReachability struct {
	source externalReachabilitySnapshots
}

func (r peerBackPingPublicReachability) Snapshot(ctx context.Context) publicReachabilitySnapshot {
	evidence := r.source.Snapshot(ctx)
	if !evidence.Known {
		return publicReachabilitySnapshot{source: publicReachabilitySourcePeerBackPing}
	}
	state := publicReachabilityUnreachable
	if evidence.PeerType == yagomodel.PeerSenior {
		state = publicReachabilityReachable
	}

	return publicReachabilitySnapshot{
		state:      state,
		source:     publicReachabilitySourcePeerBackPing,
		observedAt: evidence.ObservedAt,
	}
}

type externalFirstPublicReachability struct {
	external publicReachability
	direct   publicReachability
}

func (r externalFirstPublicReachability) Snapshot(ctx context.Context) publicReachabilitySnapshot {
	if r.external != nil {
		external := r.external.Snapshot(ctx)
		if external.state != publicReachabilityUnknown {
			return external
		}
	}
	if r.direct != nil {
		direct := r.direct.Snapshot(ctx)
		if direct.source == publicReachabilitySourcePinnedProbe {
			return direct
		}
		direct.state = publicReachabilityUnknown

		return direct
	}

	return publicReachabilitySnapshot{}
}
