package main

import (
	"context"

	"github.com/nikitakarpei/yacy-rwi-node/internal/bootstrap"
	"github.com/nikitakarpei/yacy-rwi-node/internal/crawling"
	"github.com/nikitakarpei/yacy-rwi-node/internal/nodestatus"
	"github.com/nikitakarpei/yacy-rwi-node/internal/peering"
	"github.com/nikitakarpei/yacy-rwi-node/internal/rwi"
	"github.com/nikitakarpei/yacy-rwi-node/internal/search"
	"github.com/nikitakarpei/yacy-rwi-node/internal/urlmeta"
)

type reportHolder struct {
	report nodestatus.Report
}

type rwiStatus struct{ holder *reportHolder }

func (s rwiStatus) Snapshot(ctx context.Context) rwi.StatusSnapshot {
	header := s.holder.report.Header(ctx)

	return rwi.StatusSnapshot{Version: header.Version, Uptime: header.Uptime}
}

type urlmetaStatus struct{ holder *reportHolder }

func (s urlmetaStatus) Snapshot(ctx context.Context) urlmeta.StatusSnapshot {
	header := s.holder.report.Header(ctx)

	return urlmeta.StatusSnapshot{Version: header.Version, Uptime: header.Uptime}
}

type searchStatus struct{ holder *reportHolder }

func (s searchStatus) Snapshot(ctx context.Context) search.StatusSnapshot {
	header := s.holder.report.Header(ctx)

	return search.StatusSnapshot{Version: header.Version, Uptime: header.Uptime}
}

type crawlingStatus struct{ holder *reportHolder }

func (s crawlingStatus) Snapshot(ctx context.Context) crawling.StatusSnapshot {
	header := s.holder.report.Header(ctx)

	return crawling.StatusSnapshot{Version: header.Version, Uptime: header.Uptime}
}

type peeringStatus struct {
	holder      *reportHolder
	networkName string
}

func (s peeringStatus) Snapshot(ctx context.Context) peering.StatusSnapshot {
	header := s.holder.report.Header(ctx)

	return peering.StatusSnapshot{
		Version:     header.Version,
		Uptime:      header.Uptime,
		NetworkName: s.networkName,
		Seed:        s.holder.report.SelfSeed(ctx),
	}
}

type bootstrapStatus struct{ holder *reportHolder }

func (s bootstrapStatus) Snapshot(ctx context.Context) bootstrap.StatusSnapshot {
	return bootstrap.StatusSnapshot{Seed: s.holder.report.SelfSeed(ctx)}
}
