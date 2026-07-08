package htmlpage

import (
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

type treeScan struct {
	hrefs    []string
	baseHref string
	noIndex  bool
	noFollow bool
}

func scanTree(root *html.Node) treeScan {
	var scan treeScan
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode {
			switch node.DataAtom {
			case atom.A:
				if href, ok := attribute(node, "href"); ok {
					scan.hrefs = append(scan.hrefs, href)
				}
			case atom.Base:
				if href, ok := attribute(node, "href"); ok && scan.baseHref == "" {
					scan.baseHref = href
				}
			case atom.Meta:
				scan.readMetaRobots(node)
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return scan
}

func (scan *treeScan) readMetaRobots(node *html.Node) {
	name, ok := attribute(node, "name")
	if !ok || !strings.EqualFold(strings.TrimSpace(name), "robots") {
		return
	}
	content, ok := attribute(node, "content")
	if !ok {
		return
	}
	for _, directive := range strings.Split(content, ",") {
		switch strings.ToLower(strings.TrimSpace(directive)) {
		case "noindex":
			scan.noIndex = true
		case "nofollow":
			scan.noFollow = true
		case "none":
			scan.noIndex = true
			scan.noFollow = true
		}
	}
}

func attribute(node *html.Node, key string) (string, bool) {
	for _, attr := range node.Attr {
		if strings.EqualFold(attr.Key, key) {
			return attr.Val, true
		}
	}
	return "", false
}
