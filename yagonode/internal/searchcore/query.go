package searchcore

import "strings"

const (
	operatorLanguageColon = "language:"
	operatorLanguageSlash = "/language/"
	operatorSite          = "site:"
	operatorInURL         = "inurl:"
	operatorTLD           = "tld:"
	operatorFileType      = "filetype:"
	operatorDate          = "/date"
	operatorNear          = "near"
)

type ParsedQuery struct {
	Terms          []string
	ExcludedTerms  []string
	IncludePhrases []string
	ExcludePhrases []string
	Language       string
	SiteHost       string
	InURL          string
	TLD            string
	FileType       string
	SortByDate     bool
	Near           bool
}

func ParseTextQuery(raw string) ParsedQuery {
	var parsed ParsedQuery
	var words []string
	for _, token := range queryTokens(raw) {
		if !parsed.addModifier(token) {
			words = append(words, token)
		}
	}
	parsed.addQueryWords(strings.Join(words, " "))

	return parsed
}

func (p *ParsedQuery) addModifier(token string) bool {
	if strings.HasPrefix(token, `"`) || strings.HasPrefix(token, "'") {
		return false
	}
	token = strings.TrimPrefix(token, "-")
	if token == "" {
		return false
	}

	lower := strings.ToLower(token)
	switch {
	case lower == operatorDate:
		p.SortByDate = true
	case lower == operatorNear:
		p.Near = true
	case strings.HasPrefix(lower, operatorLanguageColon):
		p.Language = strings.ToLower(token[len(operatorLanguageColon):])
	case strings.HasPrefix(lower, operatorLanguageSlash):
		p.Language = strings.ToLower(token[len(operatorLanguageSlash):])
	case strings.HasPrefix(lower, operatorSite):
		p.SiteHost = token[len(operatorSite):]
	case strings.HasPrefix(lower, operatorInURL):
		p.InURL = token[len(operatorInURL):]
	case strings.HasPrefix(lower, operatorTLD):
		p.TLD = strings.ToLower(token[len(operatorTLD):])
	case strings.HasPrefix(lower, operatorFileType):
		p.FileType = strings.ToLower(strings.TrimPrefix(token[len(operatorFileType):], "."))
	default:
		return false
	}

	return true
}

func queryTokens(raw string) []string {
	var (
		tokens []string
		token  strings.Builder
		quote  rune
	)
	for _, r := range raw {
		switch {
		case quote != 0:
			token.WriteRune(r)
			if r == quote {
				quote = 0
			}
		case (r == '"' || r == '\'') && token.Len() == 0:
			quote = r
			token.WriteRune(r)
		case isQuerySpace(r):
			tokens = appendToken(tokens, &token)
		default:
			token.WriteRune(r)
		}
	}

	return appendToken(tokens, &token)
}

func appendToken(tokens []string, token *strings.Builder) []string {
	value := strings.TrimSpace(token.String())
	token.Reset()
	if value != "" {
		tokens = append(tokens, value)
	}

	return tokens
}

func isQuerySpace(r rune) bool {
	return r == ' ' || r == '\n' || r == '\r' || r == '\t'
}
