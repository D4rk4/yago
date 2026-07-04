package documentsearch

import (
	"time"

	"github.com/D4rk4/yago/yagomodel"
)

type reportingMode int

const (
	reportNoMatches reportingMode = iota
	reportTermWithMostMatches
	reportRequestedTerms
)

type matchReporting struct {
	mode  reportingMode
	terms []yagomodel.Hash
}

type contentKind int

const (
	anyContent contentKind = iota
	imageContent
	audioContent
	videoContent
	applicationContent
)

type searchCriteria struct {
	terms              []yagomodel.Hash
	excludedTerms      []yagomodel.Hash
	requiredDocuments  []yagomodel.Hash
	maxResults         int
	maxTermSpread      int
	timeLimit          time.Duration
	reporting          matchReporting
	contentKind        contentKind
	strictContentKind  bool
	requiredProperties yagomodel.Bitfield
	language           string
	siteHash           string
}
