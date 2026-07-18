package yagocrawlcontract

import "unicode/utf8"

const MaximumCrawlLeaseIDBytes = 256

func ValidCrawlLeaseID(leaseID string) bool {
	return leaseID != "" && len(leaseID) <= MaximumCrawlLeaseIDBytes &&
		utf8.ValidString(leaseID)
}
