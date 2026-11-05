package pageparse

import (
	"strings"
	"testing"
)

func BenchmarkParseHTML(b *testing.B) {
	fixtures := []struct {
		name        string
		contentType string
		body        []byte
	}{
		{
			name:        "small",
			contentType: "text/html",
			body: []byte(
				`<html><head><title>Small</title></head><body><p>Hello world.</p></body></html>`,
			),
		},
		{
			name:        "malformed",
			contentType: "text/html",
			body: []byte(
				`<title>Broken Page</title><p>unclosed <div><h1>Orphan</p><img src="/img/pic.png" alt="a picture">`,
			),
		},
		{
			name:        "charset conversion",
			contentType: "text/html; charset=windows-1251",
			body:        []byte("<html><body>\xcf\xf0\xe8\xe2\xe5\xf2</body></html>"),
		},
		{
			name:        "large",
			contentType: "text/html",
			body: []byte(
				strings.Repeat(
					`<html><body><p>bounded large page content for benchmarking parse once.</p></body></html>`,
					500,
				),
			),
		},
	}
	for _, f := range fixtures {
		b.Run(f.name, func(b *testing.B) {
			for b.Loop() {
				ParseHTML("https://example.org/page", f.contentType, f.body)
			}
		})
	}
}
