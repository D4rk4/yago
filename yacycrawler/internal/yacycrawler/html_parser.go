package yacycrawler

import (
	"bytes"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
	"golang.org/x/net/html/charset"
)

func ParseHTML(rawURL, contentType string, body []byte) ParsedPage {
	reader, err := charset.NewReader(bytes.NewReader(body), contentType)
	if err != nil {
		reader = bytes.NewReader(body)
	}
	root, err := html.Parse(reader)
	if err != nil {
		return ParsedPage{URL: rawURL}
	}

	page := ParsedPage{URL: rawURL}
	var text strings.Builder
	walk(root, &page, &text)
	page.Title = strings.TrimSpace(page.Title)
	page.Text = selectText(contentType, body, text.String())
	return page
}

func selectText(contentType string, body []byte, fallback string) string {
	if main, err := extractMainContent(contentType, body); err == nil && main != "" {
		return main
	}
	return collapseSpaces(fallback)
}

func walk(node *html.Node, page *ParsedPage, text *strings.Builder) {
	switch node.Type {
	case html.ElementNode:
		switch node.DataAtom {
		case atom.Script, atom.Style, atom.Noscript, atom.Template:
			return
		case atom.Html:
			if page.Language == "" {
				page.Language = attr(node, "lang")
			}
		case atom.Title:
			if page.Title == "" {
				page.Title = textContent(node)
			}
		case atom.A:
			if href := attr(node, "href"); href != "" {
				page.Links = append(page.Links, href)
			}
		}
	case html.TextNode:
		text.WriteString(node.Data)
		text.WriteByte(' ')
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		walk(child, page, text)
	}
}

func attr(node *html.Node, name string) string {
	for _, a := range node.Attr {
		if a.Key == name {
			return a.Val
		}
	}
	return ""
}

func textContent(node *html.Node) string {
	var builder strings.Builder
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == html.TextNode {
			builder.WriteString(child.Data)
		}
	}
	return strings.TrimSpace(builder.String())
}

func collapseSpaces(text string) string {
	return strings.Join(strings.Fields(text), " ")
}
