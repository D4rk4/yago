package tavilyapi

import (
	"strings"
	"time"
)

func responsePublishedDate(req SearchRequest, date string) string {
	if normalizedTopic(req.Topic) != "news" {
		return ""
	}
	for _, layout := range []string{"20060102", time.DateOnly} {
		parsed, err := time.Parse(layout, strings.TrimSpace(date))
		if err == nil {
			return parsed.Format(time.DateOnly)
		}
	}

	return ""
}
