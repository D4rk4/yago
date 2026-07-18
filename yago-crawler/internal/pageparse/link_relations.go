package pageparse

import "strings"

func hasLinkRelation(value string, relation string) bool {
	for _, token := range linkRelationTokens(value) {
		if strings.EqualFold(token, relation) {
			return true
		}
	}
	return false
}

func linkRelationTokens(value string) []string {
	return strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\f'
	})
}

func followableLinks(page ParsedPage) []string {
	if len(page.FollowableLinks) != 0 || len(page.NoFollowLinks) != 0 {
		return page.FollowableLinks
	}
	return page.Links
}
