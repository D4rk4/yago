package visitintake

import (
	"github.com/google/uuid"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
)

func crawlOrderFromVisit(
	visitedPage string,
	profile yacycrawlcontract.CrawlProfile,
) yacycrawlcontract.CrawlOrder {
	return yacycrawlcontract.CrawlOrder{
		OrderID:  uuid.NewString(),
		Profile:  profile,
		SeedURLs: []string{visitedPage},
	}
}
