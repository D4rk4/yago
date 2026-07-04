package extractfetch

import (
	"strings"

	"golang.org/x/net/html"
)

func extract(doc *html.Node) Content {
	var title string
	var text strings.Builder

	var walk func(node *html.Node, inBody bool)
	walk = func(node *html.Node, inBody bool) {
		if skipElement(node) {
			return
		}
		if node.Type == html.ElementNode {
			switch node.Data {
			case "title":
				if title == "" {
					title = strings.TrimSpace(nodeText(node))
				}
			case "body":
				inBody = true
			}
		}
		if inBody && node.Type == html.TextNode {
			appendCollapsed(&text, node.Data)
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child, inBody)
		}
	}
	walk(doc, false)

	return Content{Title: title, Text: strings.TrimSpace(text.String())}
}

func skipElement(node *html.Node) bool {
	if node.Type != html.ElementNode {
		return false
	}
	switch node.Data {
	case "script", "style", "noscript", "template", "svg":
		return true
	default:
		return false
	}
}

func nodeText(node *html.Node) string {
	var builder strings.Builder
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == html.TextNode {
			builder.WriteString(child.Data)
		}
	}

	return builder.String()
}

func appendCollapsed(builder *strings.Builder, raw string) {
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		return
	}
	if builder.Len() > 0 {
		builder.WriteByte(' ')
	}
	builder.WriteString(strings.Join(fields, " "))
}
