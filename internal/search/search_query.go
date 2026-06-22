// Package search owns the search endpoint: it maps the wire request to a query,
// filters and ranks postings drawn from the rwi posting scanner, joins matching
// URL metadata from urlmeta, and encodes the wire response. It holds no storage of
// its own and reaches every collection only through published module ports.
package search

import (
	"fmt"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

type AbstractMode int

const (
	AbstractNone AbstractMode = iota
	AbstractAuto
	AbstractExplicit
)

type AbstractRequest struct {
	Mode  AbstractMode
	Words []yacymodel.Hash
}

type Filters struct {
	ContentDomain    string
	StrictContentDom bool
	TimezoneOffset   int
	Language         string
	Modifier         string
	Prefer           string
	Filter           string
	Constraint       string
	Profile          string
	SiteHost         string
	SiteHash         string
	Author           string
	Collection       string
	FileType         string
	Protocol         string
	Partitions       int
}

type Query struct {
	Words       []yacymodel.Hash
	Exclude     []yacymodel.Hash
	URLs        []yacymodel.Hash
	MaxResults  int
	MaxDistance int
	MaxTime     time.Duration
	Abstracts   AbstractRequest
	Filters     Filters
}

type Result struct {
	Resources  []yacymodel.URIMetadataRow
	JoinCount  int
	SearchTime time.Duration
	References []string
	WordCounts map[yacymodel.Hash]int
	Abstracts  map[yacymodel.Hash]string
}

func (q Query) joinLanguage() string {
	return parseSearchModifier(q.Filters.Modifier).Language
}

func (q Query) joinSiteHash() (string, error) {
	if q.Filters.SiteHash != "" {
		return q.Filters.SiteHash, nil
	}
	siteHost := parseSearchModifier(q.Filters.Modifier).SiteHost
	if siteHost == "" {
		return "", nil
	}
	hash, err := yacymodel.HashURLHost(siteHost)
	if err != nil {
		return "", fmt.Errorf("join site hash: %w", err)
	}
	hostHash, err := hash.HostHash()
	if err != nil {
		return "", fmt.Errorf("join site hash: %w", err)
	}

	return hostHash, nil
}
