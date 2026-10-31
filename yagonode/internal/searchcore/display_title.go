package searchcore

import (
	"encoding/json"
	"strings"
)

func DisplayTitle(title string) string {
	var values []json.RawMessage
	if err := json.Unmarshal([]byte(title), &values); err != nil {
		return title
	}
	if strings.TrimSpace(title) == "null" || values == nil {
		return title
	}
	for _, rawValue := range values {
		var value string
		if err := json.Unmarshal(rawValue, &value); err == nil && strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
