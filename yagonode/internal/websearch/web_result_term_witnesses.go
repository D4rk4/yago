package websearch

import (
	"net/url"
	"strings"
	"unicode"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

const (
	maximumVerificationTitleRunes   = 1024
	maximumVerificationSnippetRunes = 8192
	maximumVerificationURLRunes     = 4096
	maximumVerificationTokens       = 2048
)

type verificationSpan struct {
	start int
	end   int
}

type verificationToken struct {
	span verificationSpan
	text string
}

func coveredDistinctTerms(result searchcore.Result, terms []string) int {
	requirements := distinctVerificationTerms(terms)
	if len(requirements) == 0 {
		return 0
	}
	text := verificationText(result)
	tokens := verificationTokens(text)
	spanIdentities := make(map[verificationSpan]int, len(tokens))
	for _, token := range tokens {
		verificationSpanIdentity(spanIdentities, token.span)
	}
	edges := make([][]int, len(requirements))
	for requirement, term := range requirements {
		if usesUnsegmentedScript(term) {
			edges[requirement] = unsegmentedTermWitnesses(
				text,
				term,
				spanIdentities,
			)
			continue
		}
		for _, token := range tokens {
			if searchcore.TokenMatchesTerm(token.text, term) {
				edges[requirement] = append(
					edges[requirement],
					verificationSpanIdentity(spanIdentities, token.span),
				)
			}
		}
	}

	return maximumDistinctWitnesses(edges, len(spanIdentities))
}

func distinctVerificationTerms(terms []string) []string {
	distinct := make([]string, 0, len(terms))
	seen := make(map[string]struct{}, len(terms))
	for _, term := range terms {
		term = strings.ToLower(strings.TrimSpace(term))
		if term == "" {
			continue
		}
		if _, found := seen[term]; found {
			continue
		}
		seen[term] = struct{}{}
		distinct = append(distinct, term)
	}

	return distinct
}

func verificationText(result searchcore.Result) string {
	decodedURL, err := url.QueryUnescape(result.URL)
	if err != nil {
		decodedURL = result.URL
	}

	return strings.ToLower(strings.Join([]string{
		boundedVerificationRunes(result.Title, maximumVerificationTitleRunes),
		boundedVerificationRunes(decodedURL, maximumVerificationURLRunes),
		boundedVerificationRunes(result.Snippet, maximumVerificationSnippetRunes),
	}, " "))
}

func boundedVerificationRunes(text string, limit int) string {
	count := 0
	for index := range text {
		if count == limit {
			return text[:index]
		}
		count++
	}

	return text
}

func verificationTokens(text string) []verificationToken {
	tokens := make([]verificationToken, 0, min(maximumVerificationTokens, len(text)/4))
	start := -1
	for index, character := range text {
		if unicode.IsLetter(character) || unicode.IsNumber(character) ||
			unicode.IsMark(character) {
			if start < 0 {
				start = index
			}
			continue
		}
		if start >= 0 {
			tokens = append(tokens, verificationToken{
				span: verificationSpan{start: start, end: index},
				text: text[start:index],
			})
			if len(tokens) == maximumVerificationTokens {
				return tokens
			}
			start = -1
		}
	}
	if start >= 0 && len(tokens) < maximumVerificationTokens {
		tokens = append(tokens, verificationToken{
			span: verificationSpan{start: start, end: len(text)},
			text: text[start:],
		})
	}

	return tokens
}

func usesUnsegmentedScript(term string) bool {
	for _, character := range term {
		if unicode.In(
			character,
			unicode.Han,
			unicode.Hangul,
			unicode.Hiragana,
			unicode.Katakana,
			unicode.Thai,
			unicode.Lao,
			unicode.Khmer,
			unicode.Myanmar,
		) {
			return true
		}
	}

	return false
}

func unsegmentedTermWitnesses(
	text string,
	term string,
	spanIdentities map[verificationSpan]int,
) []int {
	witnesses := make([]int, 0, 1)
	for offset := 0; offset <= len(text)-len(term); {
		found := strings.Index(text[offset:], term)
		if found < 0 {
			break
		}
		start := offset + found
		end := start + len(term)
		witnesses = append(
			witnesses,
			verificationSpanIdentity(
				spanIdentities,
				verificationSpan{start: start, end: end},
			),
		)
		offset = end
	}

	return witnesses
}

func verificationSpanIdentity(
	identities map[verificationSpan]int,
	span verificationSpan,
) int {
	if identity, found := identities[span]; found {
		return identity
	}
	identity := len(identities)
	identities[span] = identity

	return identity
}

func maximumDistinctWitnesses(edges [][]int, witnessTotal int) int {
	assigned := make([]int, witnessTotal)
	for index := range assigned {
		assigned[index] = -1
	}
	matched := 0
	for requirement := range edges {
		seen := make([]bool, witnessTotal)
		if assignDistinctWitness(requirement, edges, assigned, seen) {
			matched++
		}
	}

	return matched
}

func assignDistinctWitness(
	requirement int,
	edges [][]int,
	assigned []int,
	seen []bool,
) bool {
	for _, witness := range edges[requirement] {
		if seen[witness] {
			continue
		}
		seen[witness] = true
		if assigned[witness] < 0 || assignDistinctWitness(
			assigned[witness],
			edges,
			assigned,
			seen,
		) {
			assigned[witness] = requirement

			return true
		}
	}

	return false
}
