package nodestatus

import (
	"net/http"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/internal/httpguard"
)

type Module struct {
	Report Report
	Query  http.Handler
}

func New(
	id Identity,
	guard httpguard.RequestGuard,
	rwi RWICounter,
	urls URLCounter,
) Module {
	report := newReport(id, rwi, urls, time.Now)

	return Module{
		Report: report,
		Query:  queryEndpoint{guard: guard, report: report, rwi: rwi, urls: urls},
	}
}
