package pageparse

import (
	"strings"

	"github.com/go-shiori/dom"
	"golang.org/x/net/html"
)

// RobotsDirectives reads the noindex and nofollow signals from a robots
// directive value — a <meta name="robots"> content attribute or an
// X-Robots-Tag response header. Matching is a case-insensitive substring scan
// over the whole value (YaCy ContentScraper parity), so comma-separated
// directive lists and agent-prefixed header forms ("googlebot: noindex") all
// register; "none" is shorthand for both directives.
func RobotsDirectives(value string) (noindex, nofollow bool) {
	folded := strings.ToLower(value)
	if strings.Contains(folded, "none") {
		return true, true
	}

	return strings.Contains(folded, "noindex"), strings.Contains(folded, "nofollow")
}

// readMetaRobots folds every <meta name="robots"> tag's directives together:
// any tag asking noindex or nofollow wins. Other meta names (description,
// googlebot) are ignored.
func readMetaRobots(root *html.Node) (noindex, nofollow bool) {
	for _, meta := range dom.GetElementsByTagName(root, "meta") {
		if !strings.EqualFold(dom.GetAttribute(meta, "name"), "robots") {
			continue
		}
		metaNoindex, metaNofollow := RobotsDirectives(dom.GetAttribute(meta, "content"))
		noindex = noindex || metaNoindex
		nofollow = nofollow || metaNofollow
	}

	return noindex, nofollow
}
