package pipeline

import (
	"strings"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/pageparse"
)

const (
	httpDateConfidence    = 0.7
	sitemapDateConfidence = 0.6
)

func pageWithSourceDate(
	page pageparse.ParsedPage,
	httpModified time.Time,
	sitemapModified time.Time,
) pageparse.ParsedPage {
	if !page.ModifiedAt.IsZero() {
		return page
	}
	modified := httpModified
	confidence := httpDateConfidence
	source := "http-last-modified"
	if modified.IsZero() {
		modified = sitemapModified
		confidence = sitemapDateConfidence
		source = "sitemap-lastmod"
	}
	if modified.IsZero() {
		return page
	}
	page.ModifiedAt = modified.UTC()
	if page.DateConfidence <= 0 {
		page.DateConfidence = confidence
	} else {
		page.DateConfidence = min(page.DateConfidence, confidence)
	}
	if page.DateSource == "" {
		page.DateSource = source
	} else if !strings.Contains(page.DateSource, source) {
		page.DateSource += "+" + source
	}

	return page
}
