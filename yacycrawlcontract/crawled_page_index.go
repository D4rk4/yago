package yacycrawlcontract

import (
	"encoding/json"
	"fmt"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

type CrawledPageIndex struct {
	SourceURL     string
	Provenance    []byte
	ProfileHandle string
	Postings      []yacymodel.RWIPosting
	Metadata      []yacymodel.URIMetadataRow
}

func MarshalCrawledPageIndex(index CrawledPageIndex) ([]byte, error) {
	data, err := json.Marshal(index)
	if err != nil {
		return nil, fmt.Errorf("marshal crawled page index: %w", err)
	}
	return data, nil
}

func UnmarshalCrawledPageIndex(data []byte) (CrawledPageIndex, error) {
	var index CrawledPageIndex
	if err := json.Unmarshal(data, &index); err != nil {
		return CrawledPageIndex{}, fmt.Errorf("unmarshal crawled page index: %w", err)
	}
	return index, nil
}
