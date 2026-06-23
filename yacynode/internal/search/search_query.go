package search

import (
	"fmt"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

type abstractMode int

const (
	abstractNone abstractMode = iota
	abstractAuto
	abstractExplicit
)

type abstractSpec struct {
	Mode  abstractMode
	Words []yacymodel.Hash
}

type searchFilters struct {
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

type searchQuery struct {
	Words         []yacymodel.Hash
	Exclude       []yacymodel.Hash
	URLs          []yacymodel.Hash
	MaxResults    int
	MaxDistance   int
	MaxTime       time.Duration
	Abstracts     abstractSpec
	searchFilters searchFilters
}

type searchResult struct {
	Resources  []yacymodel.URIMetadataRow
	JoinCount  int
	SearchTime time.Duration
	References []string
	WordCounts map[yacymodel.Hash]int
	Abstracts  map[yacymodel.Hash]string
}

func (q searchQuery) joinLanguage() string {
	return parseSearchModifier(q.searchFilters.Modifier).Language
}

func (q searchQuery) joinSiteHash() (string, error) {
	if q.searchFilters.SiteHash != "" {
		return q.searchFilters.SiteHash, nil
	}
	siteHost := parseSearchModifier(q.searchFilters.Modifier).SiteHost
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
