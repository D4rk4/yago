package documentsearch

import (
	"time"

	"github.com/D4rk4/yago/yacymodel"
)

type reportingMode int

const (
	reportNoMatches reportingMode = iota
	reportTermWithMostMatches
	reportRequestedTerms
)

type matchReporting struct {
	mode  reportingMode
	terms []yacymodel.Hash
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
	terms              []yacymodel.Hash
	excludedTerms      []yacymodel.Hash
	requiredDocuments  []yacymodel.Hash
	maxResults         int
	maxTermSpread      int
	timeLimit          time.Duration
	reporting          matchReporting
	contentKind        contentKind
	strictContentKind  bool
	requiredProperties yacymodel.Bitfield
	language           string
	siteHash           string
}
