package yacycrawlcontract

import (
	"encoding/json"
	"fmt"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

const PostingsPerMessageLimit = 1000

type CrawledPageIndexMessage struct {
	CanonicalURL string
	Metadata     []yacymodel.URIMetadataRow `json:",omitempty"`
	Postings     []yacymodel.RWIPosting     `json:",omitempty"`
}

func MarshalCrawledPageIndexMessage(message CrawledPageIndexMessage) ([]byte, error) {
	data, err := json.Marshal(message)
	if err != nil {
		return nil, fmt.Errorf("marshal crawled page index message: %w", err)
	}
	return data, nil
}

func UnmarshalCrawledPageIndexMessage(data []byte) (CrawledPageIndexMessage, error) {
	var message CrawledPageIndexMessage
	if err := json.Unmarshal(data, &message); err != nil {
		return CrawledPageIndexMessage{}, fmt.Errorf(
			"unmarshal crawled page index message: %w",
			err,
		)
	}
	return message, nil
}
