package searchcore

import (
	"slices"
	"strings"
	"unicode/utf8"
)

const queryGoalSeparators = ":;#*`!$%()=?^<>/&_"

func (p *ParsedQuery) addQueryWords(raw string) {
	words := strings.ToLower(strings.TrimSpace(raw))
	for _, separator := range queryGoalSeparators {
		words = strings.ReplaceAll(words, string(separator), " ")
	}

	parseQueryPhrases(words, &p.IncludePhrases, &p.ExcludePhrases)
	for _, phrase := range p.IncludePhrases {
		parseQueryPhrases(phrase, &p.Terms, &p.Terms)
	}
	for _, phrase := range p.ExcludePhrases {
		parseQueryPhrases(phrase, &p.ExcludedTerms, &p.ExcludedTerms)
	}
}

func parseQueryPhrases(s string, include, exclude *[]string) {
	for len(s) > 0 {
		phrase, included, rest, exhausted := nextQueryPhrase(s)
		if exhausted {
			return
		}
		s = rest
		if phrase == "" {
			continue
		}
		if included {
			appendUniquePhrase(include, phrase)
		} else {
			appendUniquePhrase(exclude, phrase)
		}
	}

	pruneSingleRunePhrases(include)
}

func nextQueryPhrase(s string) (phrase string, included bool, rest string, exhausted bool) {
	p := 0
	for p < len(s) && s[p] == ' ' {
		p++
	}
	s = s[p:]
	if len(s) == 0 {
		return "", false, "", true
	}

	included = true
	switch s[0] {
	case '-':
		included = false
		s = s[1:]
	case '+':
		s = s[1:]
	}
	if len(s) == 0 {
		return "", false, "", true
	}

	stop := byte(' ')
	if s[0] == '"' || s[0] == '\'' {
		stop = s[0]
		s = s[1:]
	}

	for p < len(s) && s[p] != stop {
		p++
	}
	phrase = s[:min(p, len(s))]
	if stop != ' ' {
		if p < len(s) && s[p] == stop {
			p++
		}
		phrase = s[:p-1]
	}
	if p < len(s) {
		rest = s[p:]
	}

	return phrase, included, rest, false
}

func appendUniquePhrase(phrases *[]string, phrase string) {
	if !slices.Contains(*phrases, phrase) {
		*phrases = append(*phrases, phrase)
	}
}

func pruneSingleRunePhrases(phrases *[]string) {
	single, multiple := false, false
	for _, phrase := range *phrases {
		if utf8.RuneCountInString(phrase) == 1 {
			single = true
		} else {
			multiple = true
		}
	}
	if !single || !multiple {
		return
	}

	kept := make([]string, 0, len(*phrases))
	for _, phrase := range *phrases {
		if utf8.RuneCountInString(phrase) > 1 {
			kept = append(kept, phrase)
		}
	}
	*phrases = kept
}
