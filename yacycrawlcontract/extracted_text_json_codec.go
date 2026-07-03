package yacycrawlcontract

import (
	"encoding/json"
	"fmt"
)

func MarshalExtractedText(text ExtractedText) ([]byte, error) {
	data, err := json.Marshal(text)
	if err != nil {
		return nil, fmt.Errorf("marshal extracted text: %w", err)
	}
	return data, nil
}

func UnmarshalExtractedText(data []byte) (ExtractedText, error) {
	var text ExtractedText
	if err := json.Unmarshal(data, &text); err != nil {
		return ExtractedText{}, fmt.Errorf("unmarshal extracted text: %w", err)
	}
	return text, nil
}
