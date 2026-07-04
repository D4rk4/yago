package yagonode

import (
	"context"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/nodestatus"
)

type peeringStatus struct {
	report      nodestatus.Report
	networkName string
}

func (s peeringStatus) NetworkName(context.Context) string {
	return s.networkName
}

func (s peeringStatus) SelfSeed(ctx context.Context) yagomodel.Seed {
	return s.report.SelfSeed(ctx)
}
