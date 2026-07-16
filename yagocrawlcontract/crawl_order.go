package yagocrawlcontract

type CrawlOrder struct {
	Provenance []byte
	Priority   CrawlOrderPriority
	Profile    CrawlProfile
	Requests   []CrawlRequest
}
