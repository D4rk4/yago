package yacycrawlcontract

import (
	"encoding/json"
	"fmt"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

const PostingsPerSegmentLimit = 1000

type CrawledPageIndexSegment struct {
	CanonicalURL string
	Metadata     []yacymodel.URIMetadataRow `json:",omitempty"`
	Postings     []yacymodel.RWIPosting     `json:",omitempty"`
}

func MarshalCrawledPageIndexSegment(segment CrawledPageIndexSegment) ([]byte, error) {
	data, err := json.Marshal(segment)
	if err != nil {
		return nil, fmt.Errorf("marshal crawled page index segment: %w", err)
	}
	return data, nil
}

func UnmarshalCrawledPageIndexSegment(data []byte) (CrawledPageIndexSegment, error) {
	var segment CrawledPageIndexSegment
	if err := json.Unmarshal(data, &segment); err != nil {
		return CrawledPageIndexSegment{}, fmt.Errorf(
			"unmarshal crawled page index segment: %w",
			err,
		)
	}
	return segment, nil
}
