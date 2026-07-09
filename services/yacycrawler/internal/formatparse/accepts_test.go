package formatparse

import (
	"testing"
)

func allOn() Toggles {
	return Toggles{
		Text: true, XMLFeeds: true, PDF: true, Office: true,
		Images: true, Audio: true, Misc: true, Archives: true,
	}
}

// TestAcceptsEveryFamilyByMIMEAndExtension pins CRAWL-17 for the whole
// registry: with its toggle on, each family is accepted both by its MIME type
// and by its URL extension (even under application/octet-stream); with the
// toggle off the same document is rejected.
func TestAcceptsEveryFamilyByMIMEAndExtension(t *testing.T) {
	off := func(on Toggles, name string) Toggles {
		switch name {
		case "text":
			on.Text = false
		case "xmlfeeds":
			on.XMLFeeds = false
		case "pdf":
			on.PDF = false
		case "office":
			on.Office = false
		case "images":
			on.Images = false
		case "audio":
			on.Audio = false
		case "misc":
			on.Misc = false
		case "archives":
			on.Archives = false
		}

		return on
	}
	cases := []struct {
		familyName string
		mime       string
		url        string
	}{
		{"text", "text/csv", "https://a.example/data.csv"},
		{"xmlfeeds", "application/rss+xml", "https://a.example/feed.rss"},
		{"pdf", "application/pdf", "https://a.example/doc.pdf"},
		{"office", "application/msword", "https://a.example/file.docx"},
		{"images", "image/png", "https://a.example/pic.png"},
		{"audio", "audio/mpeg", "https://a.example/song.mp3"},
		{"misc", "application/x-bittorrent", "https://a.example/file.torrent"},
		{"archives", "application/zip", "https://a.example/bundle.zip"},
	}
	for _, testCase := range cases {
		if !Accepts(testCase.url, testCase.mime, allOn()) {
			t.Fatalf("%s: mime %q must be accepted with the toggle on",
				testCase.familyName, testCase.mime)
		}
		if !Accepts(testCase.url, "application/octet-stream", allOn()) {
			t.Fatalf("%s: extension of %q must rescue octet-stream",
				testCase.familyName, testCase.url)
		}
		disabled := off(allOn(), testCase.familyName)
		if Accepts(testCase.url, testCase.mime, disabled) {
			t.Fatalf("%s: must be rejected with the toggle off", testCase.familyName)
		}
	}
}

// TestAcceptsHTMLAndUnknownTypes pins the boundary cases: the HTML core is
// always on regardless of toggles, unknown text degrades like Parse does, and
// unknown binary types are honestly rejected.
func TestAcceptsHTMLAndUnknownTypes(t *testing.T) {
	none := Toggles{}
	if !Accepts("https://a.example/page.html", "text/html", none) ||
		!Accepts("https://a.example/", "application/xhtml+xml", none) ||
		!Accepts("https://a.example/page.php", "", none) {
		t.Fatal("the HTML core must always be accepted")
	}
	if !Accepts("https://a.example/readme", "text/x-readme", none) {
		t.Fatal("unknown text must degrade through the HTML parser")
	}
	if Accepts("https://a.example/blob.bin", "application/x-proprietary", none) {
		t.Fatal("unknown binary types must be rejected")
	}
	if Accepts("https://a.example/doc.pdf?x=1#frag", "application/pdf", none) {
		t.Fatal("pdf with the toggle off must be rejected despite query/fragment")
	}
	if !Accepts("https://a.example/doc.pdf?x=1#frag", "application/pdf",
		Toggles{PDF: true}) {
		t.Fatal("query/fragment must not hide the extension")
	}
}
