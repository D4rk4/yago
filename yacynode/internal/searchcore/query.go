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
	Terms         []string
	ExcludedTerms []string
	Language      string
	SiteHost      string
	InURL         string
	TLD           string
	FileType      string
	SortByDate    bool
	Near          bool
}

func ParseTextQuery(raw string) ParsedQuery {
	var parsed ParsedQuery
	for _, token := range queryTokens(raw) {
		parsed.addToken(token)
	}

	return parsed
}

func (p *ParsedQuery) addToken(token string) {
	excluded := strings.HasPrefix(token, "-")
	token = strings.TrimPrefix(token, "-")
	if token == "" {
		return
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
		p.addTerms(token, excluded)
	}
}

func (p *ParsedQuery) addTerms(raw string, excluded bool) {
	for _, term := range strings.Fields(raw) {
		if excluded {
			p.ExcludedTerms = append(p.ExcludedTerms, term)
		} else {
			p.Terms = append(p.Terms, term)
		}
	}
}

func queryTokens(raw string) []string {
	clean := strings.NewReplacer("<", " ", ">", " ").Replace(raw)
	var (
		tokens []string
		token  strings.Builder
		quoted bool
	)
	for _, r := range clean {
		switch {
		case r == '"':
			quoted = !quoted
		case !quoted && isQuerySpace(r):
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
