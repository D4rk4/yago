package pageindex_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawler/internal/pageindex"
	"github.com/D4rk4/yago/yagocrawler/internal/pageparse"
	"github.com/D4rk4/yago/yagomodel"
)

func TestBuildDocumentCapsValidAnchorsAndImages(t *testing.T) {
	anchors := make(
		[]pageparse.OutboundAnchor,
		yagocrawlcontract.MaximumDocumentAnchors+2,
	)
	anchors[0].TargetURL = strings.Repeat(
		"u",
		yagocrawlcontract.MaximumCrawlURLBytes+1,
	)
	for index := 1; index < len(anchors); index++ {
		anchors[index].TargetURL = fmt.Sprintf("https://example.org/anchor/%d", index)
	}
	images := make(
		[]pageparse.ImageMetadata,
		yagocrawlcontract.MaximumDocumentImages+2,
	)
	images[0].URL = strings.Repeat(
		"u",
		yagocrawlcontract.MaximumCrawlURLBytes+1,
	)
	for index := 1; index < len(images); index++ {
		images[index].URL = fmt.Sprintf("https://example.org/image/%d", index)
	}

	document := pageindex.BuildDocument(
		pageparse.ParsedPage{
			URL:             "https://example.org/page",
			OutboundAnchors: anchors,
			Images:          images,
		},
		pageparse.PageStats{},
		yagomodel.URIMetadataRow{},
		time.Time{},
	)
	if len(document.OutboundAnchors) != yagocrawlcontract.MaximumDocumentAnchors {
		t.Fatalf("outbound anchors = %d", len(document.OutboundAnchors))
	}
	lastAnchor := document.OutboundAnchors[len(document.OutboundAnchors)-1].TargetURL
	if lastAnchor != fmt.Sprintf(
		"https://example.org/anchor/%d",
		yagocrawlcontract.MaximumDocumentAnchors,
	) {
		t.Fatalf("last outbound anchor = %q", lastAnchor)
	}
	if len(document.Images) != yagocrawlcontract.MaximumDocumentImages {
		t.Fatalf("images = %d", len(document.Images))
	}
	lastImage := document.Images[len(document.Images)-1].URL
	if lastImage != fmt.Sprintf(
		"https://example.org/image/%d",
		yagocrawlcontract.MaximumDocumentImages,
	) {
		t.Fatalf("last image = %q", lastImage)
	}
}
