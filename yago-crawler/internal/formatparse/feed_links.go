package formatparse

import (
	"encoding/xml"
	"net/url"
	"strings"
)

const xmlNamespace = "http://www.w3.org/XML/1998/namespace"

var (
	xmlBaseAttribute   = xml.Name{Space: xmlNamespace, Local: "base"}
	relAttribute       = xml.Name{Local: "rel"}
	hrefAttribute      = xml.Name{Local: "href"}
	permalinkAttribute = xml.Name{Local: "isPermaLink"}
)

func (entry *feedEntry) article() string {
	if entry.articleURL != "" || !entry.guidIsPermalink {
		return entry.articleURL
	}

	return permalinkGUIDTarget(entry.guid.completeText())
}

func entryLinkTarget(base url.URL, rel, target string) string {
	if rel != "" && rel != "alternate" {
		return ""
	}
	target = strings.TrimSpace(target)
	if target == "" {
		return ""
	}
	reference, err := url.Parse(target)
	if err != nil {
		return ""
	}

	return webArticleURL(base.ResolveReference(reference))
}

func permalinkGUIDTarget(guid string) string {
	reference, err := url.Parse(strings.TrimSpace(guid))
	if err != nil {
		return ""
	}

	return webArticleURL(reference)
}

func webArticleURL(target *url.URL) string {
	if target.Scheme != "http" && target.Scheme != "https" {
		return ""
	}

	return target.String()
}

func withXMLBase(inherited url.URL, element xml.StartElement) url.URL {
	return resolveBase(inherited, attributeValue(element, xmlBaseAttribute))
}

func resolveBase(inherited url.URL, reference string) url.URL {
	parsed, err := url.Parse(strings.TrimSpace(reference))
	if err != nil {
		return inherited
	}

	return *inherited.ResolveReference(parsed)
}

func attributeValue(element xml.StartElement, name xml.Name) string {
	for _, attribute := range element.Attr {
		if attribute.Name == name {
			return attribute.Value
		}
	}

	return ""
}
