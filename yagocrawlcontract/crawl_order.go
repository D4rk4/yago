package yagocrawlcontract

type CrawlOrder struct {
	Provenance []byte
	Profile    CrawlProfile
	Requests   []CrawlRequest
}
