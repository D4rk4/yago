package websearch

import (
	"strings"

	"github.com/D4rk4/yago/yagonode/internal/queryidentifier"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func resultHasExactIdentifiers(result searchcore.Result, terms []string) bool {
	tokens := verificationTokens(verificationText(result))
	for _, term := range terms {
		term = strings.ToLower(strings.TrimSpace(term))
		if !queryidentifier.MixedAlphanumeric(term) {
			continue
		}
		found := false
		for _, token := range tokens {
			if token.text == term {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	return true
}
