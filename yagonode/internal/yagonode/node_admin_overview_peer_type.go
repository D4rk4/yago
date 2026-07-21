package yagonode

import (
	"context"

	"github.com/D4rk4/yago/yagomodel"
)

type localPeerClassification interface {
	PeerType(context.Context) (yagomodel.PeerType, bool)
}

func (s overviewSource) withPeerType(source localPeerClassification) overviewSource {
	s.peerType = source

	return s
}

func (s overviewSource) readPeerType(ctx context.Context) string {
	if s.peerType != nil {
		peerType, current := s.peerType.PeerType(ctx)
		if current {
			switch peerType {
			case yagomodel.PeerSenior, yagomodel.PeerPrincipal:
				return yagomodel.PeerSenior.String()
			case yagomodel.PeerJunior:
				return yagomodel.PeerJunior.String()
			}
		}
	}

	return "unknown"
}
