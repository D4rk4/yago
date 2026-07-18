package tavilyapi

import "strings"

func requestedContentFormat(format string) string {
	normalized := strings.ToLower(strings.TrimSpace(format))
	if normalized == "" {
		return "markdown"
	}

	return normalized
}
