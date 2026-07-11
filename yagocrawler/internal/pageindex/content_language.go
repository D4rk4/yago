package pageindex

import "strings"

func NormalizeLanguage(lang string) string {
	normalized, ok := languageCode(lang)
	if ok {
		return normalized
	}
	return "en"
}

func languageCode(lang string) (string, bool) {
	lang = strings.ToLower(strings.TrimSpace(lang))
	if len(lang) < 2 || lang[0] < 'a' || lang[0] > 'z' || lang[1] < 'a' || lang[1] > 'z' {
		return "", false
	}
	return lang[:2], true
}
