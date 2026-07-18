package pageparse

import (
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/go-shiori/dom"
	"golang.org/x/net/html"
)

type pageDateCandidate struct {
	published  time.Time
	modified   time.Time
	confidence float64
	source     string
}

func readPageDates(root *html.Node) (time.Time, time.Time, float64, string) {
	candidates := []pageDateCandidate{
		readJSONLDDates(root),
		readItemPropertyDates(root),
		readMetaDates(root),
	}

	var published time.Time
	var modified time.Time
	confidence := 1.0
	sources := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		used := false
		if published.IsZero() && !candidate.published.IsZero() {
			published = candidate.published
			used = true
		}
		if modified.IsZero() && !candidate.modified.IsZero() {
			modified = candidate.modified
			used = true
		}
		if used {
			confidence = min(confidence, candidate.confidence)
			sources = append(sources, candidate.source)
		}
	}
	if published.IsZero() && modified.IsZero() {
		return time.Time{}, time.Time{}, 0, ""
	}

	return published.UTC(), modified.UTC(), confidence, strings.Join(sources, "+")
}

func readJSONLDDates(root *html.Node) pageDateCandidate {
	for _, script := range dom.GetElementsByTagName(root, "script") {
		mediaType, _, _ := strings.Cut(strings.ToLower(dom.GetAttribute(script, "type")), ";")
		if strings.TrimSpace(mediaType) != "application/ld+json" {
			continue
		}
		var value any
		if json.Unmarshal([]byte(dom.TextContent(script)), &value) != nil {
			continue
		}
		published, modified := datesFromJSONValue(value)
		if !published.IsZero() || !modified.IsZero() {
			return pageDateCandidate{
				published: published, modified: modified, confidence: 1, source: "json-ld",
			}
		}
	}

	return pageDateCandidate{}
}

func datesFromJSONValue(value any) (time.Time, time.Time) {
	switch typed := value.(type) {
	case []any:
		for _, child := range typed {
			published, modified := datesFromJSONValue(child)
			if !published.IsZero() || !modified.IsZero() {
				return published, modified
			}
		}
	case map[string]any:
		published := dateFromJSONProperty(typed, "datepublished")
		modified := dateFromJSONProperty(typed, "datemodified")
		if !published.IsZero() || !modified.IsZero() {
			return published, modified
		}
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			published, modified = datesFromJSONValue(typed[key])
			if !published.IsZero() || !modified.IsZero() {
				return published, modified
			}
		}
	}

	return time.Time{}, time.Time{}
}

func dateFromJSONProperty(values map[string]any, property string) time.Time {
	for key, value := range values {
		if !strings.EqualFold(key, property) {
			continue
		}
		text, ok := value.(string)
		if !ok {
			return time.Time{}
		}

		return parsePageDate(text)
	}

	return time.Time{}
}

func readItemPropertyDates(root *html.Node) pageDateCandidate {
	var candidate pageDateCandidate
	for _, element := range dom.QuerySelectorAll(root, "[itemprop]") {
		value := dom.GetAttribute(element, "content")
		if value == "" {
			value = dom.GetAttribute(element, "datetime")
		}
		for _, property := range strings.Fields(dom.GetAttribute(element, "itemprop")) {
			switch {
			case strings.EqualFold(property, "datePublished"):
				candidate.published = parsePageDate(value)
			case strings.EqualFold(property, "dateModified"):
				candidate.modified = parsePageDate(value)
			}
		}
	}
	if !candidate.published.IsZero() || !candidate.modified.IsZero() {
		candidate.confidence = 0.9
		candidate.source = "itemprop"
	}

	return candidate
}

func readMetaDates(root *html.Node) pageDateCandidate {
	var candidate pageDateCandidate
	for _, meta := range dom.GetElementsByTagName(root, "meta") {
		property := strings.ToLower(strings.TrimSpace(dom.GetAttribute(meta, "property")))
		if property == "" {
			property = strings.ToLower(strings.TrimSpace(dom.GetAttribute(meta, "name")))
		}
		value := dom.GetAttribute(meta, "content")
		switch property {
		case "article:published_time", "og:published_time", "datepublished":
			if candidate.published.IsZero() {
				candidate.published = parsePageDate(value)
			}
		case "article:modified_time", "og:modified_time", "datemodified":
			if candidate.modified.IsZero() {
				candidate.modified = parsePageDate(value)
			}
		}
	}
	if !candidate.published.IsZero() || !candidate.modified.IsZero() {
		candidate.confidence = 0.8
		candidate.source = "meta"
	}

	return candidate
}

func parsePageDate(value string) time.Time {
	value = strings.TrimSpace(value)
	for _, layout := range []string{
		time.RFC3339Nano,
		"2006-01-02T15:04:05",
		"2006-01-02",
	} {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			return parsed
		}
	}

	return time.Time{}
}
