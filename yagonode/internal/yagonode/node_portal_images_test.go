package yagonode

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func TestPortalSourceProxiesImageVertical(t *testing.T) {
	t.Parallel()

	searcher := &stubPortalSearcher{response: searchcore.Response{
		TotalResults: 1,
		Results: []searchcore.Result{{
			Title: "Pictured",
			URL:   "https://a.example/p.html",
			Images: []searchcore.ResultImage{
				{URL: "https://a.example/shot.png", Alt: "Shot"},
			},
		}},
	}}

	results, err := newPortalSource(searcher).Search(context.Background(), "go", "image", 0, 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if searcher.gotRequest.ContentDomain != searchcore.ContentDomainImage {
		t.Fatalf("content domain = %q, want image", searcher.gotRequest.ContentDomain)
	}
	images := results.Results[0].Images
	if len(images) != 1 ||
		images[0].ProxyURL != "/imgproxy?u=https%3A%2F%2Fa.example%2Fshot.png" ||
		images[0].PageURL != "https://a.example/p.html" || images[0].Alt != "Shot" {
		t.Fatalf("images = %+v, want the proxied ref", images)
	}
}
