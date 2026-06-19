package yacycrawlcontract

import (
	"encoding/json"
	"fmt"
)

func MarshalCrawlOrder(order CrawlOrder) ([]byte, error) {
	data, err := json.Marshal(order)
	if err != nil {
		return nil, fmt.Errorf("marshal crawl order: %w", err)
	}
	return data, nil
}

func UnmarshalCrawlOrder(data []byte) (CrawlOrder, error) {
	var order CrawlOrder
	if err := json.Unmarshal(data, &order); err != nil {
		return CrawlOrder{}, fmt.Errorf("unmarshal crawl order: %w", err)
	}
	return order, nil
}
