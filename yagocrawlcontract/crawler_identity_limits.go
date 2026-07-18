package yagocrawlcontract

import "unicode/utf8"

const (
	MaximumCrawlerWorkerIdentityBytes  = 256
	MaximumCrawlerSessionIdentityBytes = 256
)

func ValidCrawlerWorkerIdentity(identity string) bool {
	return identity != "" && len(identity) <= MaximumCrawlerWorkerIdentityBytes &&
		utf8.ValidString(identity)
}

func ValidCrawlerSessionIdentity(identity string) bool {
	return identity != "" && len(identity) <= MaximumCrawlerSessionIdentityBytes &&
		utf8.ValidString(identity)
}
