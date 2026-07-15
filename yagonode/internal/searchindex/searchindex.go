package searchindex

import (
	"context"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

type SearchIndex interface {
	Index(ctx context.Context, doc documentstore.Document) error
	Delete(ctx context.Context, docID string) error
	Search(ctx context.Context, req SearchRequest) (SearchResultSet, error)
	Stats(ctx context.Context) (IndexStats, error)
}

type SearchEvidenceSource interface {
	SearchEvidence(
		ctx context.Context,
		req SearchRequest,
		results []SearchResult,
	) ([]SearchResult, error)
}

type SearchRequest struct {
	Query              string
	ExcludeTerms       []string
	Phrases            []string
	MaxResults         int
	IncludeRaw         bool
	SafeSearch         bool
	IncludeDomain      []string
	ExcludeDomain      []string
	Language           string
	Since              time.Time
	Until              time.Time
	Weights            RankingWeights
	Explain            bool
	IncludeFieldScores bool
	// IncludePositions asks the backend to return matched-term positions per
	// field (bleve locations) in each result's FieldTermPositions, so a caller
	// can measure query-term coverage and proximity over the document itself
	// rather than the truncated snippet.
	IncludePositions bool
	CandidateOnly    bool
	Fuzzy            bool
	Relaxed          bool
	// Author keeps only documents whose extracted author metadata contains this
	// text (case-insensitive).
	Author string
	// Terms carries the parsed query words for the proximity filter; Near keeps
	// only documents where every term appears within one small token window.
	Terms []string
	Near  bool
	// ExpansionTerms are optional recall terms (pseudo-relevance feedback): they
	// boost documents that already match every required query term and never
	// admit one that does not.
	ExpansionTerms     []string
	MinimumTermMatches int
	// WithFacets asks for facet counts over every matching document.
	WithFacets bool
	// ContentDomain narrows results to a media vertical (image/audio/video/app);
	// empty, "text", and "all" accept every document.
	ContentDomain string
	// MinDate and MaxDate bound results by document date when non-zero.
	MinDate time.Time
	MaxDate time.Time
	// FileType keeps only documents whose file type — classified from the
	// Content-Type with the URL extension as a fallback — matches; InURL keeps
	// only documents whose URL contains the substring; TLD keeps only documents
	// whose host sits under the top-level domain. They live here, beside the
	// other post-retrieval filters, so the navigation facets are counted over the
	// same set the results are drawn from.
	FileType string
	InURL    string
	TLD      string
}

type SearchResultSet struct {
	// Facets carries the facet groups when the request asked for them.
	Facets  []FacetGroup
	Results []SearchResult
	Total   int
}

type SearchResult struct {
	DocumentID        string
	ClusterID         string
	RepresentativeURL string
	Title             string
	URL               string
	Snippet           string
	RawContent        string
	Score             float64
	Explanation       string
	PublishedDate     time.Time
	DateConfidence    float64
	// Author is the document's extracted author metadata (doc.Metadata["author"]),
	// surfaced for the yacysearch RSS dc:creator field; empty when the document
	// carried none.
	Author string
	// Keywords is the document's extracted keyword metadata
	// (doc.Metadata["keywords"]), surfaced for the yacysearch RSS dc:subject field.
	Keywords string
	// Publisher is the document's extracted publisher metadata
	// (doc.Metadata["publisher"]), surfaced for the yacysearch RSS dc:publisher field.
	Publisher            string
	Language             string
	Quality              float64
	QualityKnown         bool
	SpamRisk             float64
	FunctionWordFraction float64
	SymbolFraction       float64
	AlphabeticFraction   float64
	UniqueTokenFraction  float64
	Analyzer             string
	EvidenceReady        bool
	SafetyRating         documentstore.SafetyRating
	ExplicitProbability  float64
	SafetyConfidence     float64
	StrictScore          float64
	StrictRank           int
	RelaxedScore         float64
	RelaxedRank          int
	// Proximity is the SDM unordered-window feature in [0,1]: the fraction of
	// adjacent query-word pairs that co-occur within a small token window of the
	// document text, computed at result mapping (query-dependent); the searcher
	// folds it into the score by the RankingWeights.Proximity weight.
	Proximity        float64
	OrderedProximity float64
	FieldScores      map[string]float64
	// FieldTermPositions maps each field to the 1-based token positions of every
	// matched query term (from bleve locations). It is populated only when
	// IncludePositions is set, and lets the reranker score coverage and proximity
	// over the document.
	FieldTermPositions map[string]map[string][]int
	// ContentType is the document's stored Content-Type, carried so the file
	// type is classified from what the document actually is, not only its URL
	// extension.
	ContentType string
	// Size is the indexed text length in bytes, carried onto the result so local
	// hits report a size like peer results do.
	Size int
	// Images carries the document's extracted images for the image vertical.
	Images                 []ResultImage
	quotedPhrasePreference float64
	relaxedPassageEvidence bool
}

// ResultImage is one extracted page image surfaced by the image vertical.
type ResultImage struct {
	URL string
	Alt string
}

type IndexStats struct {
	Documents int
	Backend   string
	UpdatedAt time.Time
}
