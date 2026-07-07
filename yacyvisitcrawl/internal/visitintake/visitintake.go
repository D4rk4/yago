// Package visitintake receives a visited-page visit, attempts to place one
// crawl order for it, and redirects the browser onward without waiting for
// that attempt's outcome. MountVisitIntake is its only surface;
// CrawlOrderPlacement is the port an attempt leaves through.
package visitintake

import (
	"net/http"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
)

const PathVisit = "/visit"

type CrawlOrderPlacement interface {
	Attempt(order yacycrawlcontract.CrawlOrder)
}

type VisitMetrics interface {
	VisitReceived()
	VisitRejected()
	OrderPlaced()
	OrderUnplaced()
}

func MountVisitIntake(
	mux *http.ServeMux,
	placement CrawlOrderPlacement,
	profile yacycrawlcontract.CrawlProfile,
	metrics VisitMetrics,
	maxBodyBytes int64,
) {
	mux.Handle(PathVisit, visitedPageEndpoint{
		placement:    placement,
		profile:      profile,
		metrics:      metrics,
		maxBodyBytes: maxBodyBytes,
	})
}
