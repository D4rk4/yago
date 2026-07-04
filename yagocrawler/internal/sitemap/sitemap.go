package sitemap

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"strings"
	"time"
)

type Document struct {
	URLs      []Entry
	Sitemaps  []Entry
	Truncated bool
}

type Entry struct {
	URL          string
	LastModified time.Time
}

func ParseXML(raw []byte, limit int) (Document, error) {
	var root struct {
		XMLName xml.Name
	}
	if err := xml.NewDecoder(bytes.NewReader(raw)).Decode(&root); err != nil {
		return Document{}, fmt.Errorf("decode sitemap root: %w", err)
	}

	switch root.XMLName.Local {
	case "urlset":
		return parseURLSet(raw, limit)
	case "sitemapindex":
		return parseSitemapIndex(raw, limit)
	default:
		return Document{}, fmt.Errorf("unsupported sitemap root %q", root.XMLName.Local)
	}
}

func ParseSitelist(raw []byte, limit int) Document {
	var doc Document
	for _, line := range strings.Split(string(raw), "\n") {
		value := strings.TrimSpace(line)
		if value == "" || strings.HasPrefix(value, "#") {
			continue
		}
		if reachedLimit(len(doc.URLs), limit) {
			doc.Truncated = true
			break
		}
		doc.URLs = append(doc.URLs, Entry{URL: value})
	}
	return doc
}

func parseURLSet(raw []byte, limit int) (Document, error) {
	var value struct {
		URLs []entryXML `xml:"url"`
	}
	_ = xml.Unmarshal(raw, &value)
	entries, truncated := entriesFromXML(value.URLs, limit)
	return Document{URLs: entries, Truncated: truncated}, nil
}

func parseSitemapIndex(raw []byte, limit int) (Document, error) {
	var value struct {
		Sitemaps []entryXML `xml:"sitemap"`
	}
	_ = xml.Unmarshal(raw, &value)
	entries, truncated := entriesFromXML(value.Sitemaps, limit)
	return Document{Sitemaps: entries, Truncated: truncated}, nil
}

type entryXML struct {
	Loc     string `xml:"loc"`
	LastMod string `xml:"lastmod"`
}

func entriesFromXML(values []entryXML, limit int) ([]Entry, bool) {
	capacity := len(values)
	if limit >= 0 && limit < capacity {
		capacity = limit
	}
	entries := make([]Entry, 0, capacity)
	truncated := false
	for _, value := range values {
		url := strings.TrimSpace(value.Loc)
		if url == "" {
			continue
		}
		if reachedLimit(len(entries), limit) {
			truncated = true
			break
		}
		entries = append(entries, Entry{
			URL:          url,
			LastModified: parseLastModified(value.LastMod),
		})
	}
	return entries, truncated
}

func parseLastModified(value string) time.Time {
	value = strings.TrimSpace(value)
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			return parsed
		}
	}
	return time.Time{}
}

func reachedLimit(count int, limit int) bool {
	return limit >= 0 && count >= limit
}
