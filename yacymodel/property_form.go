package yacymodel

import "strings"

func parsePropertyPairs(body string) map[string]string {
	props := make(map[string]string)
	for token := range strings.SplitSeq(body, ",") {
		token = strings.TrimSpace(token)
		pos := strings.IndexByte(token, '=')
		if pos <= 0 {
			continue
		}
		props[strings.TrimSpace(token[:pos])] = strings.TrimSpace(token[pos+1:])
	}
	return props
}
