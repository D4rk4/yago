package search

import "strings"

const (
	modifierLanguagePrefix   = "/language/"
	modifierSitePrefix       = "site:"
	modifierFileTypePrefix   = "filetype:"
	modifierAuthorPrefix     = "author:"
	modifierKeywordPrefix    = "keyword:"
	modifierCollectionPrefix = "collection:"
	modifierLanguageLength   = 2
)

var modifierProtocols = map[string]string{
	"/https": "https",
	"/http":  "http",
	"/ftp":   "ftp",
	"/smb":   "smb",
	"/file":  "file",
}

type searchModifier struct {
	Language   string
	SiteHost   string
	Protocol   string
	FileType   string
	Author     string
	Keyword    string
	Collection string
}

func parseSearchModifier(modifier string) searchModifier {
	var parsed searchModifier
	for token := range strings.FieldsSeq(modifier) {
		switch {
		case strings.HasPrefix(token, modifierLanguagePrefix):
			if code := token[len(modifierLanguagePrefix):]; len(code) == modifierLanguageLength {
				parsed.Language = strings.ToLower(code)
			}
		case modifierProtocols[token] != "":
			parsed.Protocol = modifierProtocols[token]
		case strings.HasPrefix(token, modifierSitePrefix):
			parsed.SiteHost = token[len(modifierSitePrefix):]
		case strings.HasPrefix(token, modifierFileTypePrefix):
			parsed.FileType = token[len(modifierFileTypePrefix):]
		case strings.HasPrefix(token, modifierAuthorPrefix):
			parsed.Author = strings.Trim(token[len(modifierAuthorPrefix):], "()")
		case strings.HasPrefix(token, modifierKeywordPrefix):
			parsed.Keyword = token[len(modifierKeywordPrefix):]
		case strings.HasPrefix(token, modifierCollectionPrefix):
			parsed.Collection = token[len(modifierCollectionPrefix):]
		}
	}

	return parsed
}
