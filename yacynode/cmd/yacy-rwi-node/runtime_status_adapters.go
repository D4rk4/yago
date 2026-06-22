package main

import (
	"context"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/nodestatus"
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
