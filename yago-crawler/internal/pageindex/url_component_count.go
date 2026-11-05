package pageindex

import "strings"

func urlComponentCount(rawURL string) int {
	s := rawURL
	if idx := strings.Index(s, "//"); idx > 0 {
		s = s[idx+2:]
	}
	return len(splitComponents(strings.ToLower(s)))
}

func splitComponents(s string) []string {
	if s == "" {
		return []string{""}
	}
	parts := make([]string, 0)
	start := 0
	for i := 0; i < len(s); i++ {
		if isComponentDelimiter(s[i]) {
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	parts = append(parts, s[start:])
	count := len(parts)
	for count > 0 && parts[count-1] == "" {
		count--
	}
	return parts[:count]
}

func isComponentDelimiter(b byte) bool {
	switch b {
	case ' ', '/', '(', ')', '-', ':', '_', '.', ',', '?', '!', '\'', '"':
		return true
	default:
		return false
	}
}
