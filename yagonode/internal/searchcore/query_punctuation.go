package searchcore

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

func NormalizeTextQuery(raw string) string {
	tokens := queryTokens(raw)
	for index, token := range tokens {
		var modifier ParsedQuery
		if modifier.addModifier(token) {
			continue
		}
		tokens[index] = normalizeQueryTokenDashes(token)
	}

	return strings.Join(tokens, " ")
}

func normalizeQueryTokenDashes(token string) string {
	runes := []rune(token)
	var normalized strings.Builder
	var quote rune
	negative := false
	for index, current := range runes {
		if current == '"' || current == '\'' {
			switch {
			case quote == current:
				quote = 0
			case quote == 0 && (index == 0 || negative && index == 1):
				quote = current
			}
			normalized.WriteRune(current)

			continue
		}
		if !unicode.Is(unicode.Dash, current) {
			normalized.WriteRune(current)

			continue
		}
		if current == '-' && index == 0 {
			negative = true
			normalized.WriteRune(current)

			continue
		}
		writeQuerySpace(&normalized)
		if quote == 0 && negative && queryTokenHasWordAfter(runes[index+1:]) {
			normalized.WriteRune('-')
		}
	}

	return normalized.String()
}

func queryTokenHasWordAfter(runes []rune) bool {
	afterDashes := strings.TrimLeftFunc(string(runes), func(current rune) bool {
		return unicode.Is(unicode.Dash, current)
	})
	next, _ := utf8.DecodeRuneInString(afterDashes)

	return afterDashes != "" && (unicode.IsLetter(next) || unicode.IsNumber(next))
}

func writeQuerySpace(normalized *strings.Builder) {
	if normalized.Len() > 0 && !strings.HasSuffix(normalized.String(), " ") {
		normalized.WriteRune(' ')
	}
}
