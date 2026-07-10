package filetypeclass

import "testing"

func TestCanonical(t *testing.T) {
	cases := []struct {
		name        string
		rawURL      string
		contentType string
		want        string
	}{
		{
			"pdf by content type at extension-less url",
			"https://arxiv.org/pdf/2401.12345",
			"application/pdf",
			"pdf",
		},
		{
			"html by content type with charset param",
			"https://a.example/about",
			"text/html; charset=utf-8",
			"html",
		},
		{"content type wins over url extension", "https://a.example/page.txt", "text/html", "html"},
		{
			"office docx by real ooxml mime",
			"https://a.example/d",
			"application/vnd.openxmlformats-officedocument.wordprocessingml.document",
			"docx",
		},
		{"image jpeg canonicalises to jpg", "https://a.example/p", "image/jpeg", "jpg"},
		{
			"unknown content type falls back to url ext",
			"https://a.example/song.mp3",
			"application/octet-stream",
			"mp3",
		},
		{"no content type uses url ext", "https://a.example/doc.pdf", "", "pdf"},
		{"query string trimmed before extension", "https://a.example/a.PDF?v=1", "", "pdf"},
		{"fragment trimmed before extension", "https://a.example/a.Pdf#top", "", "pdf"},
		{"overlong url extension dropped", "https://a.example/deep/file.verylongext", "", ""},
		{
			"malformed content type falls back to bare type",
			"https://a.example/x",
			"application/pdf; broken",
			"pdf",
		},
		{"nothing to classify", "https://a.example/", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Canonical(tc.rawURL, tc.contentType); got != tc.want {
				t.Fatalf("Canonical(%q, %q) = %q, want %q", tc.rawURL, tc.contentType, got, tc.want)
			}
		})
	}
}

func TestMatches(t *testing.T) {
	cases := []struct {
		name        string
		rawURL      string
		contentType string
		wanted      string
		want        bool
	}{
		{
			"pdf mime matches extension-less arxiv url",
			"https://arxiv.org/pdf/2401.12345",
			"application/pdf",
			"pdf",
			true,
		},
		{
			"pdf url extension matches with no content type",
			"https://a.example/paper.pdf",
			"",
			"pdf",
			true,
		},
		{
			"html content type matches every page",
			"https://a.example/about",
			"text/html",
			"html",
			true,
		},
		{"jpeg alias matches image/jpeg", "https://a.example/p", "image/jpeg", "jpeg", true},
		{"jpg query matches a .jpeg url", "https://a.example/p.jpeg", "", "jpg", true},
		{"htm alias matches an html page", "https://a.example/x", "text/html", "htm", true},
		{
			"wanted with leading dot is normalised",
			"https://a.example/x",
			"application/pdf",
			".pdf",
			true,
		},
		{"uppercase wanted matches", "https://a.example/x", "application/pdf", "PDF", true},
		{"mismatch rejected", "https://a.example/a.pdf", "text/html", "zip", false},
		{"empty wanted rejected", "https://a.example/a.pdf", "application/pdf", "", false},
		{"bare-dot wanted rejected", "https://a.example/a.pdf", "application/pdf", ".", false},
		{"no signal rejects", "https://a.example/", "", "pdf", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Matches(tc.rawURL, tc.contentType, tc.wanted); got != tc.want {
				t.Fatalf("Matches(%q, %q, %q) = %v, want %v",
					tc.rawURL, tc.contentType, tc.wanted, got, tc.want)
			}
		})
	}
}
