package extractfetch

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/html"
)

func TestFetchAndPageCollectImages(t *testing.T) {
	body := `<html><head><title>Images</title></head><body>` +
		`<img src="/one.png"><img src="https://cdn.example/two.jpg#part">` +
		`<img src="data:image/png,x"><img src="/one.png"></body></html>`
	fetcher := New(htmlClient(body, "text/html"), time.Second, 0)
	content, err := fetcher.Fetch(context.Background(), "https://site.example/base/")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	page, err := fetcher.FetchPage(context.Background(), "https://site.example/base/")
	if err != nil {
		t.Fatalf("FetchPage: %v", err)
	}
	want := []string{"https://site.example/one.png", "https://cdn.example/two.jpg"}
	if strings.Join(content.Images, "|") != strings.Join(want, "|") ||
		strings.Join(page.Images, "|") != strings.Join(want, "|") {
		t.Fatalf("content=%v page=%v", content.Images, page.Images)
	}
}

func TestCollectImagesRejectsInvalidBaseURL(t *testing.T) {
	doc, err := html.Parse(strings.NewReader(`<img src="/one.png">`))
	if err != nil {
		t.Fatalf("parse document: %v", err)
	}
	if images := collectImages(doc, "://bad"); images != nil {
		t.Fatalf("images = %v, want nil", images)
	}
}

func TestCollectImagesBoundsUniqueSources(t *testing.T) {
	var body strings.Builder
	body.WriteString("<html><body>")
	for image := 0; image < maximumPageImages+1; image++ {
		body.WriteString(`<img src="/image-` + strconv.Itoa(image) + `.png">`)
	}
	body.WriteString("</body></html>")
	doc, err := html.Parse(strings.NewReader(body.String()))
	if err != nil {
		t.Fatalf("parse document: %v", err)
	}
	images := collectImages(doc, "https://site.example/")
	if len(images) != maximumPageImages {
		t.Fatalf("images = %d, want %d", len(images), maximumPageImages)
	}
	if images[0] != "https://site.example/image-0.png" ||
		images[maximumPageImages-1] != "https://site.example/image-19.png" {
		t.Fatalf("bounded images = %v", images)
	}
}

func TestImageSourceWithoutSourceAttribute(t *testing.T) {
	doc, err := html.Parse(strings.NewReader(`<img alt="description">`))
	if err != nil {
		t.Fatalf("parse document: %v", err)
	}
	images := collectImages(doc, "https://site.example/")
	if len(images) != 0 {
		t.Fatalf("images = %v, want empty", images)
	}
}
