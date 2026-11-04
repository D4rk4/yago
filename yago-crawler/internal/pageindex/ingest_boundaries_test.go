package pageindex_test

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/pageindex"
	"github.com/D4rk4/yago/yago-crawler/internal/pageparse"
	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagomodel"
)

func TestBuildPageStatsBoundsIndexingWork(t *testing.T) {
	words := make([]string, yagocrawlcontract.MaximumDocumentWords+100)
	for position := range words {
		words[position] = fmt.Sprintf("word%d", position)
	}
	links := make([]string, yagocrawlcontract.MaximumDocumentOutlinks+100)
	for position := range links {
		links[position] = fmt.Sprintf("/page/%d", position)
	}
	page := pageparse.ParsedPage{
		URL:   "https://example.org/",
		Title: strings.Repeat("title ", 300),
		Text:  strings.Join(words, " "),
		Links: links,
	}
	stats := pageindex.BuildPageStats(page)
	artifacts := pageindex.BuildRWIPageArtifacts(page, stats, time.Time{})
	if got := artifacts.Metadata.Properties["wc"]; got != fmt.Sprint(
		yagocrawlcontract.MaximumDocumentWords,
	) {
		t.Fatalf("word count = %s", got)
	}
	if len(artifacts.Postings) == 0 {
		t.Fatal("expected bounded text to produce postings")
	}
	if got := artifacts.Postings[0].Properties[yagomodel.ColTitleWordCount]; got != yagomodel.FormatRWICardinal(
		255,
	) {
		t.Fatalf("title word count = %s", got)
	}
	if len(stats.LocalLinks)+len(stats.ExternalLinks) !=
		yagocrawlcontract.MaximumDocumentOutlinks {
		t.Fatalf("links = %d", len(stats.LocalLinks)+len(stats.ExternalLinks))
	}
}

func TestBuildDocumentBoundsTransportFields(t *testing.T) {
	const pageURL = "https://example.org/page"
	headings := make([]string, yagocrawlcontract.MaximumDocumentHeadings+1)
	for position := range headings {
		headings[position] = strings.Repeat("h", yagocrawlcontract.MaximumDocumentHeadingBytes+1)
	}
	outlinks := make([]string, yagocrawlcontract.MaximumDocumentOutlinks+1)
	for position := range outlinks {
		outlinks[position] = fmt.Sprintf("https://example.org/%d", position)
	}
	outlinks[0] = strings.Repeat("u", yagocrawlcontract.MaximumCrawlURLBytes+1)
	document := pageindex.BuildDocument(
		pageparse.ParsedPage{
			URL:      pageURL,
			Title:    strings.Repeat("t", yagocrawlcontract.MaximumDocumentTitleBytes+1),
			Headings: headings,
			Description: strings.Repeat(
				"d",
				yagocrawlcontract.MaximumDocumentMetadataBytes+1,
			),
			Text: "bounded document",
			OutboundAnchors: []pageparse.OutboundAnchor{
				{TargetURL: strings.Repeat("u", yagocrawlcontract.MaximumCrawlURLBytes+1)},
				{TargetURL: "https://example.org/anchor"},
			},
			Images: []pageparse.ImageMetadata{
				{URL: strings.Repeat("u", yagocrawlcontract.MaximumCrawlURLBytes+1)},
				{URL: "https://example.org/image.png"},
			},
		},
		pageparse.PageStats{LocalLinks: outlinks},
		yagomodel.URIMetadataRow{},
		time.Time{},
	)
	if document.NormalizedURL != pageURL ||
		len(document.Title) != yagocrawlcontract.MaximumDocumentTitleBytes ||
		len(document.Headings) != yagocrawlcontract.MaximumDocumentHeadings ||
		len(document.Headings[0]) != yagocrawlcontract.MaximumDocumentHeadingBytes ||
		len(document.Outlinks) != yagocrawlcontract.MaximumDocumentOutlinks ||
		document.Outlinks[0] != "https://example.org/1" ||
		len(document.OutboundAnchors) != 1 ||
		document.OutboundAnchors[0].TargetURL != "https://example.org/anchor" ||
		len(document.Images) != 1 ||
		document.Images[0].URL != "https://example.org/image.png" ||
		len(document.Metadata["description"]) !=
			yagocrawlcontract.MaximumDocumentMetadataBytes {
		t.Fatalf("document bounds = %#v", document)
	}
}

func TestIndexBuilderRejectsOverlongIdentityURL(t *testing.T) {
	_, err := pageindex.NewIndexBuilder().Build(
		pageparse.ParsedPage{
			URL: "https://example.org/" + strings.Repeat(
				"x",
				yagocrawlcontract.MaximumCrawlURLBytes,
			),
			Text: "content",
		},
		pageparse.PageStats{},
	)
	if err == nil {
		t.Fatal("overlong page URL must be rejected")
	}
}

func TestBuildDocumentFallsBackFromOverlongCanonicalURL(t *testing.T) {
	const pageURL = "https://example.org/page"
	document := pageindex.BuildDocument(
		pageparse.ParsedPage{
			URL: pageURL,
			CanonicalURL: "https://example.org/" + strings.Repeat(
				"x",
				yagocrawlcontract.MaximumCrawlURLBytes,
			),
		},
		pageparse.PageStats{},
		yagomodel.URIMetadataRow{},
		time.Time{},
	)
	if document.CanonicalURL != pageURL {
		t.Fatalf("canonical URL = %q, want %q", document.CanonicalURL, pageURL)
	}
}

func TestBuildPostingsBoundsTermsAndSetsDocumentType(t *testing.T) {
	tokens := make([]string, yagocrawlcontract.MaximumIngestPostings+100)
	for position := range tokens {
		tokens[position] = fmt.Sprintf("term%d", position)
	}
	postings := pageindex.BuildPostings(
		pageparse.ParsedPage{
			URL:      "https://example.org/",
			Language: "en",
			Text:     strings.Join(tokens, " "),
		},
		pageparse.PageStats{},
	)
	if len(postings) != yagocrawlcontract.MaximumIngestPostings {
		t.Fatalf("postings = %d", len(postings))
	}
	wantType := strconv.FormatUint(uint64(yagomodel.DocTypeText), 10)
	for _, posting := range postings {
		if posting.Properties[yagomodel.ColDocType] != wantType {
			t.Fatalf("document type = %q", posting.Properties[yagomodel.ColDocType])
		}
	}
}
