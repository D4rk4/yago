package yagonode

import (
	"context"

	"github.com/D4rk4/yago/yacymodel"
	"github.com/D4rk4/yago/yacynode/internal/nodestatus"
)

type peeringStatus struct {
	report      nodestatus.Report
	networkName string
}

func (s peeringStatus) NetworkName(context.Context) string {
	return s.networkName
}

func (s peeringStatus) SelfSeed(ctx context.Context) yacymodel.Seed {
	return s.report.SelfSeed(ctx)
}
