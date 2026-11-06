package formatparse

import (
	"net/url"
	"strings"
)

// selectItemLink chooses the single article address for a completed entry.
// It prefers nonempty HTTP(S) absent or alternate links, then an RSS GUID only
// when there is no usable link and its isPermaLink is not false. It never
// selects a self or enclosure link.
func selectItemLink(entry *feedEntry) string {
	if article := usableLink(entry); article != "" {
		return article
	}

	if entry.guid.set && entry.guid.perma != "false" {
		if isHTTPTarget(entry.guid.value) {
			return resolveBase(entry.base, entry.guid.value)
		}
	}

	return ""
}

// usableLink returns the first nonempty HTTP(S) absent or alternate link, or
// an empty string when none exists.
func usableLink(entry *feedEntry) string {
	for _, link := range entry.links {
		rel := strings.ToLower(strings.TrimSpace(link.rel))
		if rel == "self" || rel == "enclosure" {
			continue
		}
		if rel != "" && rel != "alternate" {
			continue
		}
		target := link.href
		if target == "" {
			target = link.text
		}
		if link.base != "" {
			target = resolveBase(link.base, target)
		}
		if isHTTPTarget(target) {
			return target
		}
	}

	return ""
}

// isHTTPTarget reports whether target is a nonempty HTTP(S) reference.
func isHTTPTarget(target string) bool {
	target = strings.TrimSpace(target)
	if target == "" {
		return false
	}
	lower := strings.ToLower(target)

	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
}

// resolveBase resolves a relative base against the entry base. An empty local
// returns the parent base unchanged, an empty parent returns the local
// untouched.
func resolveBase(parent string, local string) string {
	local = strings.TrimSpace(local)
	if local == "" {
		return parent
	}
	if parent == "" {
		return local
	}
	parsedParent, err := url.Parse(parent)
	if err != nil {
		return local
	}
	parsedLocal, err := url.Parse(local)
	if err != nil {
		return local
	}

	return parsedParent.ResolveReference(parsedLocal).String()
}
