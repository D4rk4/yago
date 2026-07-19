package adminui

import "context"

type DocumentDetailSource interface {
	DocumentDetail(ctx context.Context, key string) (DocumentDetail, bool, error)
}

type DocumentDetail struct {
	Key                     string
	Extraction              DocumentExtractionDetail
	URL                     string
	NormalizedURL           string
	CanonicalURL            string
	RepresentativeURL       string
	Title                   string
	Headings                []string
	ContentPreview          string
	ContentBytes            int
	ContentPreviewTruncated bool
	RawContentReference     string
	Language                string
	ContentType             string
	FetchStatus             string
	FetchedAt               string
	IndexedAt               string
	PublishedAt             string
	ModifiedAt              string
	FirstSeenAt             string
	ContentChangedAt        string
	DateConfidence          float64
	DateSource              string
	ContentHash             string
	ClusterID               string
	Quality                 DocumentQualityDetail
	Safety                  DocumentSafetyDetail
	Metadata                []DocumentMetadataDetail
	HeadingsTotal           int
	MetadataTotal           int
	Outlinks                []string
	OutlinksTotal           int
	Inlinks                 []DocumentLinkDetail
	InlinksTotal            int
	OutboundAnchors         []DocumentLinkDetail
	OutboundAnchorsTotal    int
	Images                  []DocumentImageDetail
	ImagesTotal             int
}

type DocumentExtractionDetail struct {
	Generation uint64
	Current    uint64
}

type DocumentQualityDetail struct {
	Known                bool
	Score                float64
	FunctionWordFraction float64
	SymbolFraction       float64
	AlphabeticFraction   float64
	UniqueTokenFraction  float64
	SpamRisk             float64
}

type DocumentSafetyDetail struct {
	Rating              string
	ExplicitProbability float64
	Confidence          float64
}

type DocumentMetadataDetail struct {
	Name  string
	Value string
}

type DocumentLinkDetail struct {
	URL           string
	Text          string
	NoFollow      bool
	UserGenerated bool
	Sponsored     bool
}

type DocumentImageDetail struct {
	URL     string
	AltText string
}
