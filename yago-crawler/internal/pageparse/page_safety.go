package pageparse

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/go-shiori/dom"
	"golang.org/x/net/html"
)

const (
	maximumRatingValues = 16
	maximumRatingRunes  = 256
)

func readSafetyLabels(root *html.Node) SafetyLabels {
	labels := SafetyLabels{RatingValues: ratingValues(root)}
	for _, script := range dom.GetElementsByTagName(root, "script") {
		mediaType, _, _ := strings.Cut(strings.ToLower(dom.GetAttribute(script, "type")), ";")
		if strings.TrimSpace(mediaType) != "application/ld+json" {
			continue
		}
		var value any
		if json.Unmarshal([]byte(dom.TextContent(script)), &value) != nil {
			continue
		}
		if familyFriendly, found := familyFriendlyFromJSON(value); found {
			labels.FamilyFriendly = &familyFriendly

			break
		}
	}

	return labels
}

func ratingValues(root *html.Node) []string {
	values := make([]string, 0)
	seen := map[string]struct{}{}
	for _, meta := range dom.GetElementsByTagName(root, "meta") {
		name := firstNonEmptyAttribute(meta, "name", "property", "http-equiv")
		if !strings.EqualFold(strings.TrimSpace(name), "rating") {
			continue
		}
		value := boundedRatingValue(dom.GetAttribute(meta, "content"))
		key := strings.ToLower(value)
		if value == "" {
			continue
		}
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		values = append(values, value)
		if len(values) == maximumRatingValues {
			break
		}
	}

	return values
}

func firstNonEmptyAttribute(node *html.Node, names ...string) string {
	for _, name := range names {
		if value := dom.GetAttribute(node, name); value != "" {
			return value
		}
	}

	return ""
}

func boundedRatingValue(value string) string {
	value = collapseSpaces(value)
	runes := []rune(value)
	if len(runes) > maximumRatingRunes {
		return string(runes[:maximumRatingRunes])
	}

	return value
}

func familyFriendlyFromJSON(value any) (bool, bool) {
	switch typed := value.(type) {
	case []any:
		return familyFriendlyFromArray(typed)
	case map[string]any:
		return familyFriendlyFromObject(typed)
	}

	return false, false
}

func familyFriendlyFromArray(values []any) (bool, bool) {
	for _, child := range values {
		if familyFriendly, found := familyFriendlyFromJSON(child); found {
			return familyFriendly, true
		}
	}

	return false, false
}

func familyFriendlyFromObject(values map[string]any) (bool, bool) {
	for key, child := range values {
		if strings.EqualFold(key, "isFamilyFriendly") {
			if familyFriendly, ok := child.(bool); ok {
				return familyFriendly, true
			}
		}
	}

	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	return familyFriendlyFromSortedChildren(values, keys)
}

func familyFriendlyFromSortedChildren(values map[string]any, keys []string) (bool, bool) {
	for _, key := range keys {
		if familyFriendly, found := familyFriendlyFromJSON(values[key]); found {
			return familyFriendly, true
		}
	}

	return false, false
}
