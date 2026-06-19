package yacycrawlcontract

import (
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

type CrawlRequest struct {
	URL           string
	ReferrerURL   string
	AnchorName    string
	Depth         int
	ProfileHandle string
	Initiator     yacymodel.Hash
	AppDate       time.Time
}
