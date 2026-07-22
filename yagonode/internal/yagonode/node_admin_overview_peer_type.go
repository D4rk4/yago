package yagonode

import (
	"context"

	"github.com/D4rk4/yago/yagomodel"
)

type localPeerClassification interface {
	PublishedPeerType(context.Context) yagomodel.PeerType
}

func (s overviewSource) withPeerType(source localPeerClassification) overviewSource {
	s.peerType = source

	return s
}

func (s overviewSource) readPeerType(ctx context.Context) string {
	if s.peerType != nil {
		switch s.peerType.PublishedPeerType(ctx) {
		case yagomodel.PeerSenior, yagomodel.PeerPrincipal:
			return yagomodel.PeerSenior.String()
		case yagomodel.PeerJunior:
			return yagomodel.PeerJunior.String()
		case yagomodel.PeerVirgin:
			return yagomodel.PeerVirgin.String()
		}
	}

	return "unknown"
}
