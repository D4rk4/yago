package nodestatus

import (
	"net/http"

	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/httpguard"
)

type Module struct {
	Report Report
	Query  http.Handler
}

func New(
	id Identity,
	live Liveness,
	guard httpguard.RequestGuard,
	rwi RWICounter,
	urls URLCounter,
) Module {
	report := newReport(id, live, rwi, urls)

	return Module{
		Report: report,
		Query: queryEndpoint{
			guard:   guard,
			respond: httpguard.NewWireResponder(report),
			rwi:     rwi,
			urls:    urls,
		},
	}
}
