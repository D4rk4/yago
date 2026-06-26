package pageindex

import "strings"

func NormalizeLanguage(lang string) string {
	lang = strings.ToLower(strings.TrimSpace(lang))
	if len(lang) >= 2 {
		return lang[:2]
	}
	return "en"
}
