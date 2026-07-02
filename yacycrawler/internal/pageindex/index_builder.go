package pageindex

import (
	"time"

	"github.com/D4rk4/yago/yacycrawler/internal/pageparse"
	"github.com/D4rk4/yago/yacymodel"
)

type Artifacts struct {
	Postings []yacymodel.RWIPosting
	Metadata yacymodel.URIMetadataRow
}

type IndexBuilder interface {
	Build(page pageparse.ParsedPage, stats pageparse.PageStats) (Artifacts, error)
}

type contentIndexBuilder struct {
	clock func() time.Time
}

func NewIndexBuilder() IndexBuilder {
	return &contentIndexBuilder{clock: time.Now}
}

func (b *contentIndexBuilder) Build(
	page pageparse.ParsedPage,
	stats pageparse.PageStats,
) (Artifacts, error) {
	postings := BuildPostings(page, stats)
	metadata := BuildMetadata(page, stats, b.clock())
	return Artifacts{Postings: postings, Metadata: metadata}, nil
}
