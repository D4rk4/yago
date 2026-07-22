package yagonode

import (
	"context"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/nodestatus"
)

func localPeerIsVirgin(ctx context.Context, report nodestatus.Report) bool {
	if report == nil {
		return true
	}

	switch report.PublishedPeerType(ctx) {
	case yagomodel.PeerJunior, yagomodel.PeerSenior, yagomodel.PeerPrincipal:
		return false
	default:
		return true
	}
}
