package yacycrawlcontract

import (
	"encoding/json"
	"fmt"
	"time"
)

type CrawledPage struct {
	CanonicalURL string
	Title        string
	Text         string
	CrawledAt    time.Time
	Language     string
}

func MarshalCrawledPage(page CrawledPage) ([]byte, error) {
	data, err := json.Marshal(page)
	if err != nil {
		return nil, fmt.Errorf("marshal crawled page: %w", err)
	}
	return data, nil
}

func UnmarshalCrawledPage(data []byte) (CrawledPage, error) {
	var page CrawledPage
	if err := json.Unmarshal(data, &page); err != nil {
		return CrawledPage{}, fmt.Errorf("unmarshal crawled page: %w", err)
	}
	return page, nil
}
