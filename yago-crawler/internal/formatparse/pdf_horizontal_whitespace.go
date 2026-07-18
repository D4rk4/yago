package formatparse

import (
	"strings"
	"unicode"
)

func pdfCollapseHorizontalWhitespace(text string) string {
	var normalized strings.Builder
	normalized.Grow(len(text))
	space := false
	for _, value := range text {
		if value == '\t' || unicode.Is(unicode.Zs, value) {
			if !space {
				normalized.WriteByte(' ')
			}
			space = true

			continue
		}
		_, _ = normalized.WriteRune(value)
		space = false
	}

	return normalized.String()
}
