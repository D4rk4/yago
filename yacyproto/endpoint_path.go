package yacyproto

// Paths of the /yacy/* peer-to-peer endpoints. All use plain HTTP; requests are
// HTTP form fields and responses are key=value lines.
const (
	PathHello        = "/yacy/hello.html"
	PathTransferRWI  = "/yacy/transferRWI.html"
	PathTransferURL  = "/yacy/transferURL.html"
	PathSearch       = "/yacy/search.html"
	PathQuery        = "/yacy/query.html"
	PathCrawlReceipt = "/yacy/crawlReceipt.html"
)
