package yagocrawlcontract

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	MaximumCrawlerWorkerIdentityBytes       = 256
	MaximumCrawlerWorkerIdentityPrefixBytes = MaximumCrawlerWorkerIdentityBytes - 37
	MaximumCrawlerSessionIdentityBytes      = 256
)

func ValidCrawlerWorkerIdentity(identity string) bool {
	return validCrawlerIdentity(identity, MaximumCrawlerWorkerIdentityBytes)
}

func ValidCrawlerSessionIdentity(identity string) bool {
	return validCrawlerIdentity(identity, MaximumCrawlerSessionIdentityBytes)
}

func ParseCrawlerWorkerIdentityPrefix(raw string) (string, error) {
	identity := strings.Trim(raw, " ")
	if !validCrawlerIdentity(identity, MaximumCrawlerWorkerIdentityPrefixBytes) {
		return "", fmt.Errorf(
			"crawler worker identity prefix must be one visible line between 1 and %d bytes",
			MaximumCrawlerWorkerIdentityPrefixBytes,
		)
	}

	return identity, nil
}

func validCrawlerIdentity(identity string, maximumBytes int) bool {
	return identity != "" && len(identity) <= maximumBytes && utf8.ValidString(identity) &&
		!strings.ContainsFunc(identity, func(character rune) bool {
			return unicode.In(character, unicode.Cc, unicode.Cf, unicode.Zl, unicode.Zp)
		})
}
