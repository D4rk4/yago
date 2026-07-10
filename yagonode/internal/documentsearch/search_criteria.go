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

// reportsTermCounts reports whether the mode surfaces exact per-term match totals
// (the wire indexcount keys), which a scan must count exhaustively rather than
// stopping at the per-term cap.
func (r matchReporting) reportsTermCounts() bool {
	return r.mode == reportTermWithMostMatches || r.mode == reportRequestedTerms
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
	// allowEarlyTermination lets each per-term scan stop once matchesPerTerm
	// matches are kept, skipping the tail of long posting lists. It is opt-in and
	// off by default so the scan stays exhaustive and wire-visible indexcount
	// totals exact; a caller sets it only where those per-term totals are not
	// reported downstream (the local searchcore path, and the P2P no-abstracts
	// mode).
	allowEarlyTermination bool
}
