package hostlinks

import (
	"context"

	"github.com/D4rk4/yago/yagonode/internal/hostlinkgraph"
	"github.com/D4rk4/yago/yagonode/internal/httpguard"
	"github.com/D4rk4/yago/yagoproto"
)

// HostReferenceRowDefinition is the exact rowdef string YaCy's idx.json advertises
// for host references: WebStructureGraph.hostReferenceFactory.getRow().toString().
// The host-hash column carries no {b256} encoder — only the two cardinals do — so a
// YaCy peer parsing the feed decodes the columns with the same widths and codecs.
const (
	HostReferenceRowDefinition       = hostlinkgraph.HostReferenceRowDefinition
	SnapshotHostHashBytes            = hostlinkgraph.SnapshotHostHashBytes
	MaximumSnapshotLinkedHosts       = hostlinkgraph.MaximumSnapshotLinkedHosts
	MaximumSnapshotReferencesPerHost = hostlinkgraph.MaximumSnapshotReferencesPerHost
	MaximumSnapshotReferences        = hostlinkgraph.MaximumSnapshotReferences
	MaximumSnapshotReferenceBytes    = hostlinkgraph.MaximumSnapshotReferenceBytes
)

type RuntimeStatus interface {
	Version(ctx context.Context) string
	Uptime(ctx context.Context) int
}

type IncomingHostLinks interface {
	IncomingHostLinks(ctx context.Context) Graph
}

type Graph = hostlinkgraph.Graph

type LinkedHost = hostlinkgraph.LinkedHost

type NoIncomingHostLinks struct{}

func (NoIncomingHostLinks) IncomingHostLinks(context.Context) Graph {
	return Graph{RowDefinition: HostReferenceRowDefinition}
}

func Mount(
	router httpguard.WireRouter,
	networkName string,
	status RuntimeStatus,
	links IncomingHostLinks,
) {
	httpguard.MountRawWithAdmission(
		router,
		httpguard.RawRouteAdmission[yagoproto.IndexRequest]{
			Path:      yagoproto.PathIndex,
			Methods:   yagoproto.IndexEndpointMethods,
			Parse:     yagoproto.ParseIndexRequest,
			Serve:     endpoint{networkName: networkName, status: status, links: links}.Serve,
			Admission: httpguard.NewIntakeGate(maximumConcurrentIndexResponses),
		},
	)
}
