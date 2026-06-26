package pageindex

import (
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/crawlwork"
	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

type Artifacts struct {
	Postings []yacymodel.RWIPosting
	Metadata yacymodel.URIMetadataRow
}

type IndexBuilder interface {
	Build(page crawlwork.ParsedPage, stats crawlwork.PageStats) (Artifacts, error)
}

type contentIndexBuilder struct {
	clock func() time.Time
}

func NewIndexBuilder() IndexBuilder {
	return &contentIndexBuilder{clock: time.Now}
}

func (b *contentIndexBuilder) Build(
	page crawlwork.ParsedPage,
	stats crawlwork.PageStats,
) (Artifacts, error) {
	postings, err := BuildPostings(page, stats)
	if err != nil {
		return Artifacts{}, err
	}
	metadata, err := BuildMetadata(page, stats, b.clock())
	if err != nil {
		return Artifacts{}, err
	}
	return Artifacts{Postings: postings, Metadata: metadata}, nil
}
