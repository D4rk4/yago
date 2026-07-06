package searchindex

import (
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

func TestAllowsContentDomainVerticals(t *testing.T) {
	pictured := documentstore.Document{
		NormalizedURL: "https://a.example/page.html",
		Images:        []documentstore.ImageMetadata{{URL: "https://a.example/i.png"}},
	}
	audio := documentstore.Document{NormalizedURL: "https://a.example/track.mp3"}
	app := documentstore.Document{NormalizedURL: "https://a.example/tool.apk"}
	plain := documentstore.Document{NormalizedURL: "https://a.example/doc.html"}

	if !allowsContentDomain(pictured, "image") || allowsContentDomain(plain, "image") {
		t.Fatal("image domain must require extracted images")
	}
	if !allowsContentDomain(audio, "audio") || allowsContentDomain(plain, "audio") {
		t.Fatal("audio domain must match by extension")
	}
	if !allowsContentDomain(app, "APP") {
		t.Fatal("domain matching must be case-insensitive")
	}
	if allowsContentDomain(audio, "video") {
		t.Fatal("audio file admitted into the video domain")
	}
	for _, domain := range []string{"", "text", "all"} {
		if !allowsContentDomain(plain, domain) {
			t.Fatalf("%q domain must accept every document", domain)
		}
	}
}

func TestImageVerticalCarriesResultImages(t *testing.T) {
	index, err := NewBleveMemoryIndex(t.Context(), &fakeStoredDocuments{
		documents: []documentstore.Document{
			{
				NormalizedURL: "https://a.example/pictured.html",
				Title:         "Pictured golang",
				ExtractedText: "golang with images",
				Images: []documentstore.ImageMetadata{
					{URL: "https://a.example/shot.png", AltText: "Shot"},
					{URL: ""},
				},
			},
			{
				NormalizedURL: "https://a.example/plain.html",
				Title:         "Plain golang",
				ExtractedText: "golang without images",
			},
		},
	})
	if err != nil {
		t.Fatalf("NewBleveMemoryIndex: %v", err)
	}

	result, err := index.Search(t.Context(), SearchRequest{
		Query: "golang", MaxResults: 5, ContentDomain: "image",
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if result.Total != 1 || len(result.Results) != 1 {
		t.Fatalf("image vertical results = %+v, want only the pictured page", result)
	}
	images := result.Results[0].Images
	if len(images) != 1 || images[0].URL != "https://a.example/shot.png" ||
		images[0].Alt != "Shot" {
		t.Fatalf("result images = %+v", images)
	}

	text, err := index.Search(t.Context(), SearchRequest{Query: "golang", MaxResults: 5})
	if err != nil {
		t.Fatalf("text search: %v", err)
	}
	if len(text.Results[0].Images)+len(text.Results[1].Images) != 0 {
		t.Fatal("text vertical must not carry image payloads")
	}
}
