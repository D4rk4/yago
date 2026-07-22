package documentsearch

import "strings"

const (
	operatorLanguagePrefix = "/language/"
	operatorSitePrefix     = "site:"
	operatorAuthorPrefix   = "author:"
	operatorFileTypePrefix = "filetype:"
	operatorLanguageLength = 2
)

type queryOperators struct {
	Language string
	SiteHost string
	Author   string
	FileType string
	Protocol string
}

func parseQueryOperators(query string) queryOperators {
	var parsed queryOperators
	tokens := strings.Fields(query)
	for position := 0; position < len(tokens); position++ {
		token := tokens[position]
		lower := strings.ToLower(token)
		switch {
		case strings.HasPrefix(lower, operatorLanguagePrefix):
			if code := token[len(operatorLanguagePrefix):]; len(code) == operatorLanguageLength {
				parsed.Language = strings.ToLower(code)
			}
		case strings.HasPrefix(lower, operatorSitePrefix):
			parsed.SiteHost = token[len(operatorSitePrefix):]
		case strings.HasPrefix(lower, operatorAuthorPrefix):
			parsed.Author, position = parseAuthorOperator(tokens, position)
		case strings.HasPrefix(lower, operatorFileTypePrefix):
			parsed.FileType = token[len(operatorFileTypePrefix):]
		case lower == "/http" || lower == "/https" || lower == "/ftp" ||
			lower == "/smb" || lower == "/file":
			parsed.Protocol = strings.TrimPrefix(lower, "/")
		}
	}

	return parsed
}

func parseAuthorOperator(tokens []string, position int) (string, int) {
	value := tokens[position][len(operatorAuthorPrefix):]
	if !strings.HasPrefix(value, "(") {
		return value, position
	}
	value = strings.TrimPrefix(value, "(")
	parts := []string{value}
	for !strings.HasSuffix(parts[len(parts)-1], ")") && position+1 < len(tokens) {
		position++
		parts = append(parts, tokens[position])
	}
	parts[len(parts)-1] = strings.TrimSuffix(parts[len(parts)-1], ")")

	return strings.Join(parts, " "), position
}
