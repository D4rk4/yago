package yagocrawlcontract

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
)

const (
	MaximumCrawlURLDenylistEntries     = 4096
	MaximumCrawlURLDenylistBytes       = 1 << 20
	MaximumCrawlURLDenylistDomainBytes = 253
	CrawlURLDenylistRevisionBytes      = sha256.Size
)

type CrawlURLDenylist struct {
	Revision  []byte
	ExactURLs []string
	Domains   []string
}

func NewCrawlURLDenylist(
	exactURLs []string,
	domains []string,
) (CrawlURLDenylist, error) {
	if len(exactURLs)+len(domains) > MaximumCrawlURLDenylistEntries {
		return CrawlURLDenylist{}, fmt.Errorf(
			"crawl URL denylist has %d entries, maximum %d",
			len(exactURLs)+len(domains),
			MaximumCrawlURLDenylistEntries,
		)
	}
	canonicalURLs := make([]string, 0, len(exactURLs))
	canonicalDomains := make([]string, 0, len(domains))
	encodedBytes := 0
	for _, exactURL := range exactURLs {
		exactURL = strings.TrimSpace(exactURL)
		if exactURL == "" || !utf8.ValidString(exactURL) ||
			len(exactURL) > MaximumCrawlURLBytes {
			return CrawlURLDenylist{}, fmt.Errorf("invalid crawl URL denylist URL")
		}
		encodedBytes += len(exactURL) + 5
		canonicalURLs = append(canonicalURLs, exactURL)
	}
	for _, domain := range domains {
		domain = strings.Trim(strings.ToLower(strings.TrimSpace(domain)), ".")
		if domain == "" || !utf8.ValidString(domain) ||
			len(domain) > MaximumCrawlURLDenylistDomainBytes {
			return CrawlURLDenylist{}, fmt.Errorf("invalid crawl URL denylist domain")
		}
		encodedBytes += len(domain) + 5
		canonicalDomains = append(canonicalDomains, domain)
	}
	if encodedBytes > MaximumCrawlURLDenylistBytes {
		return CrawlURLDenylist{}, fmt.Errorf(
			"crawl URL denylist uses %d bytes, maximum %d",
			encodedBytes,
			MaximumCrawlURLDenylistBytes,
		)
	}
	slices.Sort(canonicalURLs)
	slices.Sort(canonicalDomains)
	canonicalURLs = slices.Compact(canonicalURLs)
	canonicalDomains = slices.Compact(canonicalDomains)
	denylist := CrawlURLDenylist{
		ExactURLs: canonicalURLs,
		Domains:   canonicalDomains,
	}
	denylist.Revision = crawlURLDenylistRevision(denylist)

	return denylist, nil
}

func ParseCrawlURLDenylist(
	revision []byte,
	exactURLs []string,
	domains []string,
) (CrawlURLDenylist, error) {
	if !ValidCrawlURLDenylistRevision(revision) {
		return CrawlURLDenylist{}, fmt.Errorf("invalid crawl URL denylist revision")
	}
	denylist, err := NewCrawlURLDenylist(exactURLs, domains)
	if err != nil {
		return CrawlURLDenylist{}, err
	}
	if !bytes.Equal(denylist.Revision, revision) {
		return CrawlURLDenylist{}, fmt.Errorf("crawl URL denylist revision does not match payload")
	}

	return denylist, nil
}

func ValidCrawlURLDenylistRevision(revision []byte) bool {
	return len(revision) == 0 || len(revision) == CrawlURLDenylistRevisionBytes
}

func crawlURLDenylistRevision(denylist CrawlURLDenylist) []byte {
	revision := sha256.New()
	var size [4]byte
	for _, exactURL := range denylist.ExactURLs {
		_, _ = revision.Write([]byte{0})
		binary.BigEndian.PutUint32(size[:], crawlURLDenylistEntrySize(exactURL))
		_, _ = revision.Write(size[:])
		_, _ = revision.Write([]byte(exactURL))
	}
	for _, domain := range denylist.Domains {
		_, _ = revision.Write([]byte{1})
		binary.BigEndian.PutUint32(size[:], crawlURLDenylistEntrySize(domain))
		_, _ = revision.Write(size[:])
		_, _ = revision.Write([]byte(domain))
	}

	return revision.Sum(nil)
}

func crawlURLDenylistEntrySize(value string) uint32 {
	var size uint32
	for index := 0; index < len(value); index++ {
		size++
	}

	return size
}
