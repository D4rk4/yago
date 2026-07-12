package hostlinks

import (
	"context"
	"encoding/json"

	"github.com/D4rk4/yago/yagonode/internal/httpguard"
	"github.com/D4rk4/yago/yagoproto"
)

// HostReferenceRowDefinition is the exact rowdef string YaCy's idx.json advertises
// for host references: WebStructureGraph.hostReferenceFactory.getRow().toString().
// The host-hash column carries no {b256} encoder — only the two cardinals do — so a
// YaCy peer parsing the feed decodes the columns with the same widths and codecs.
const HostReferenceRowDefinition = "String h-6, Cardinal m-4 {b256}, Cardinal c-4 {b256}"

type RuntimeStatus interface {
	Version(ctx context.Context) string
	Uptime(ctx context.Context) int
}

type IncomingHostLinks interface {
	IncomingHostLinks(ctx context.Context) Graph
}

type Graph struct {
	RowDefinition string
	LinkedHosts   []LinkedHost
}

type LinkedHost struct {
	HostHash   string
	References []json.RawMessage
}

type NoIncomingHostLinks struct{}

func (NoIncomingHostLinks) IncomingHostLinks(context.Context) Graph {
	return Graph{}
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
