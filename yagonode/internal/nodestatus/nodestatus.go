// Package nodestatus owns the node's runtime status: its self-seed, the
// version/uptime header every endpoint echoes, and the query.html capacity
// answers. Its published port, Report, is the only surface other modules
// import. Live counts arrive through the RWICounter and URLCounter ports, so
// nodestatus never reads another module's schema.
package nodestatus

import (
	"context"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/httpguard"
	"github.com/D4rk4/yago/yagonode/internal/nodeidentity"
	"github.com/D4rk4/yago/yagoproto"
)

type Report interface {
	PublishedPeerClassification
	Version(ctx context.Context) string
	Uptime(ctx context.Context) int
	UptimeSeconds(ctx context.Context) int
	SelfSeed(ctx context.Context) yagomodel.Seed
}

type RWICounter interface {
	RWICount(ctx context.Context) (int, error)
	RWIURLCount(ctx context.Context, word yagomodel.Hash) (int, error)
}

type URLCounter interface {
	Count(ctx context.Context) (int, error)
}

type ReachablePeerCounter interface {
	ReachablePeerCount(ctx context.Context) int
}

type SeedQueueStatistics struct {
	Noticed      int
	NoticedKnown bool
	Offered      int
	OfferedKnown bool
}

type SeedQueueStatisticsSource interface {
	SeedQueueStatistics(ctx context.Context) SeedQueueStatistics
}

type SeedNewsSource interface {
	SeedNews(ctx context.Context) string
}

type PublishedPeerClassification interface {
	PublishedPeerType(ctx context.Context) yagomodel.PeerType
}

type TransferTotals struct {
	Known         bool
	SentWords     int64
	ReceivedWords int64
	SentURLs      int64
	ReceivedURLs  int64
}

type TransferTotalsSource interface {
	TransferTotals(ctx context.Context) TransferTotals
}

type ReportSources struct {
	RWI                RWICounter
	URLs               URLCounter
	Peers              ReachablePeerCounter
	Queues             SeedQueueStatisticsSource
	News               SeedNewsSource
	Transfers          TransferTotalsSource
	PeerClassification PublishedPeerClassification
}

const queryContentType = "text/html; charset=UTF-8"

func NewReport(id nodeidentity.Identity, sources ReportSources) Report {
	return newReport(id, time.Now, sources)
}

func MountQuery(
	router httpguard.WireRouter,
	identity nodeidentity.Identity,
	rwi RWICounter,
) {
	httpguard.MountWithContentType(
		router,
		yagoproto.PathQuery,
		yagoproto.QueryEndpointMethods,
		queryContentType,
		httpguard.WireEndpoint[yagoproto.QueryRequest, yagoproto.QueryResponse]{
			Parse: yagoproto.ParseQueryRequest,
			Serve: queryEndpoint{identity: identity, rwi: rwi}.Serve,
		},
	)
}
