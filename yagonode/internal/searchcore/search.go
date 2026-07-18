package searchcore

import (
	"context"
	"time"
)

type Source string

const (
	SourceLocal  Source = "local"
	SourceGlobal Source = "global"
	SourceRemote Source = "remote"
	SourceWeb    Source = "ddgs"
)

type ContentDomain string

const (
	ContentDomainAll   ContentDomain = "all"
	ContentDomainText  ContentDomain = "text"
	ContentDomainImage ContentDomain = "image"
	ContentDomainAudio ContentDomain = "audio"
	ContentDomainVideo ContentDomain = "video"
	ContentDomainApp   ContentDomain = "app"
)

type VerifyMode string

const (
	VerifyFalse     VerifyMode = "false"
	VerifyTrue      VerifyMode = "true"
	VerifyCacheOnly VerifyMode = "cacheonly"
	VerifyIfFresh   VerifyMode = "iffresh"
	VerifyIfExist   VerifyMode = "ifexist"
)

type SafetyRating uint8

const (
	SafetyUnknown SafetyRating = iota
	SafetyGeneral
	SafetyExplicit
)

type Request struct {
	Query          string
	SubmittedQuery string
	Terms          []string
	ExcludedTerms  []string
	Phrases        []string
	Source         Source
	Limit          int
	// Fuzzy asks the local index for approximate (edit-distance) term matching;
	// the zero-result recovery retry sets it, remote fan-out ignores it.
	Fuzzy bool
	// WithFacets asks the local index for facet counts over every match.
	WithFacets bool
	// MinDate and MaxDate bound results by document date when non-zero.
	MinDate          time.Time
	MaxDate          time.Time
	Offset           int
	ContentDomain    ContentDomain
	Language         string
	SiteHost         string
	InURL            string
	TLD              string
	FileType         string
	Author           string
	URLMaskFilter    string
	PreferMaskFilter string
	// ExpansionTerms carries recall terms mined by pseudo-relevance feedback.
	// The local index scores them as optional evidence only: they lift documents
	// that already match every required query term and never admit one that does
	// not (RM3 drift control; Lavrenko & Croft, SIGIR 2001).
	ExpansionTerms   []string
	Verify           VerifyMode
	Navigation       string
	SortByDate       bool
	Near             bool
	AllowWebFallback bool
	SafeSearch       bool
	Explain          bool
	RankingFeatures  bool
}

type Result struct {
	DocumentID                  string
	Analyzer                    string
	EvidenceReady               bool
	EvidenceRequirementOrdinals []int
	Title                       string
	URL                         string
	ClusterID                   string
	RepresentativeURL           string
	DisplayURL                  string
	Snippet                     string
	QueryMatches                []QueryMatch
	BodyQueryMatches            []QueryMatch
	Score                       float64
	Evidence                    RankingEvidence
	diversityRelevance          float64
	diversityRelevanceSet       bool
	Source                      Source
	Host                        string
	Path                        string
	File                        string
	ContentType                 string
	URLHash                     string
	Size                        int
	Date                        string
	DateConfidence              float64
	ContentDomain               ContentDomain
	Language                    string
	// Author is the document's extracted author metadata (HTML meta author),
	// surfaced for the yacysearch RSS dc:creator field; empty when the document
	// carried none or for remote results that did not include it.
	Author string
	// Keywords is the document's extracted keyword metadata, surfaced for the
	// yacysearch RSS dc:subject field; empty when absent or for remote results.
	Keywords string
	// Publisher is the document's extracted publisher metadata, surfaced for the
	// yacysearch RSS dc:publisher field; empty when absent or for remote results.
	Publisher            string
	Quality              float64
	QualityKnown         bool
	SpamRisk             float64
	FunctionWordFraction float64
	SymbolFraction       float64
	AlphabeticFraction   float64
	UniqueTokenFraction  float64
	SafetyRating         SafetyRating
	ExplicitProbability  float64
	SafetyConfidence     float64
	Proximity            float64
	// FieldScores carries the per-field BM25 sub-score contributions when the
	// searcher computed them (local results with explanation on); nil otherwise.
	FieldScores        map[string]float64
	FieldTermPositions map[string]map[string][]int
	Explanation        string
	// Images carries the page's extracted images for the image vertical.
	Images []ResultImage
}

type QueryMatch struct {
	Start int
	End   int
}

type PartialFailure struct {
	Source string `json:"source"`
	Reason string `json:"reason"`
}

type Response struct {
	Request         Request
	TotalResults    int
	Availability    ResultAvailability
	Results         []Result
	PartialFailures []PartialFailure
	// Recovered names the zero-result recovery step that produced these results
	// ("fuzzy") so surfaces can say "no exact matches; showing close matches";
	// empty for a normal answer.
	Recovered string
	// DidYouMean carries a spelling suggestion assembled from close-match titles
	// when recovery found the query terms slightly off; empty when none.
	DidYouMean string
	// Facets carries the local facet groups when the request asked for them.
	Facets []FacetGroup
}

// FacetGroup is one facet dimension with its most frequent terms among the
// locally matching documents.
type FacetGroup struct {
	Name  string
	Terms []FacetTerm
}

type FacetTerm struct {
	Term  string
	Count int
}

// ResultImage is one extracted page image surfaced by the image vertical.
type ResultImage struct {
	URL string
	Alt string
}

type Searcher interface {
	Search(context.Context, Request) (Response, error)
}
