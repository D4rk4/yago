package tavilyapi

import "strings"

func unsupportedSafeSearchDepth(depth string) bool {
	switch normalizedSearchDepth(depth) {
	case "fast", "ultra-fast":
		return true
	default:
		return false
	}
}

func normalizedSearchDepth(depth string) string {
	normalized := strings.ToLower(strings.TrimSpace(depth))
	if normalized == "" {
		return "basic"
	}

	return normalized
}
