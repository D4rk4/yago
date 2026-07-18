package formatparse

import (
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

const sampleHTML = `<html><head><title>Sample</title></head><body><p>text</p></body></html>`

func TestParseRoutesHTMLAlways(t *testing.T) {
	for _, target := range []string{
		"https://a.example/page.html",
		"https://a.example/dir/",
		"https://a.example/script.php?x=1",
	} {
		page, parsed := Parse(
			target,
			"text/html",
			[]byte(sampleHTML),
			yagocrawlcontract.FormatToggles{},
		)
		if !parsed || page.Title != "Sample" {
			t.Fatalf(
				"%s: parsed=%v title=%q, want the always-on HTML core",
				target,
				parsed,
				page.Title,
			)
		}
	}

	// The HTML MIME wins even with a foreign extension.
	if _, parsed := Parse(
		"https://a.example/download.pdf", "text/html", []byte(sampleHTML),
		yagocrawlcontract.FormatToggles{},
	); !parsed {
		t.Fatal("text/html content must parse regardless of the extension")
	}
}

func TestParseMatchedFamilyWithoutParserSkipsIndexing(t *testing.T) {
	toggles := yagocrawlcontract.DefaultFormatToggles()
	page, parsed := Parse(
		"https://a.example/report.pdf",
		"application/pdf",
		[]byte("%PDF-1.4"),
		toggles,
	)
	if parsed {
		t.Fatal("a family without an implemented parser must report unparsed")
	}
	if page.URL != "https://a.example/report.pdf" {
		t.Fatalf("unparsed page must keep its URL: %q", page.URL)
	}
}

func TestParseDisabledFamilySkips(t *testing.T) {
	toggles := yagocrawlcontract.DefaultFormatToggles()
	if _, parsed := Parse(
		"https://a.example/bundle.zip", "application/zip", []byte("PK"), toggles,
	); parsed {
		t.Fatal("archives are off by default and must not parse")
	}
	if _, parsed := Parse(
		"https://a.example/doc.txt", "text/plain", nil,
		yagocrawlcontract.FormatToggles{},
	); parsed {
		t.Fatal("a disabled family must not parse")
	}
}

func TestParseUnknownBinaryTypeStaysUnparsed(t *testing.T) {
	if _, parsed := Parse(
		"https://a.example/data.unknownext", "application/octet-stream", []byte("x"),
		yagocrawlcontract.FormatToggles{},
	); parsed {
		t.Fatal("unknown binary type reached the HTML fallback")
	}
}

func TestFamilyMatrixCoversYaCyExtensions(t *testing.T) {
	byName := map[string]family{}
	for _, entry := range families() {
		byName[entry.name] = entry
	}
	cases := map[string][]string{
		"text":     {"txt", "tex", "csv", "rtf", "msg"},
		"xmlfeeds": {"xml", "rss", "atom"},
		"pdf":      {"pdf", "ps"},
		"office": {
			"doc", "xls", "xla", "ppt", "pps", "docx", "dotx", "pptx", "ppsx",
			"potx", "xlsx", "xltx", "odt", "ods", "odp", "odg", "odc", "odf",
			"odb", "odi", "odm", "ott", "ots", "otp", "otg", "sxw", "sxc",
			"vsd", "vss", "vst", "mm",
		},
		"images": {"jpg", "jpeg", "jpe", "png", "gif", "bmp", "wbmp", "tif", "tiff", "psd", "svg"},
		"audio": {
			"mp3", "ogg", "wma", "wav", "m4a", "m4b", "m4p", "mp4",
			"aif", "aifc", "aiff", "ra", "rm", "sid",
		},
		"misc":     {"vcf", "torrent", "apk"},
		"archives": {"zip", "jar", "epub", "tar", "gz", "tgz", "bz2", "tbz", "tbz2", "xz", "txz"},
	}
	for name, extensions := range cases {
		entry, ok := byName[name]
		if !ok {
			t.Fatalf("family %q missing", name)
		}
		for _, ext := range extensions {
			if !entry.extensions[ext] {
				t.Fatalf("family %q missing extension %q", name, ext)
			}
		}
	}
}

func TestDefaultTogglesEnableAllButArchives(t *testing.T) {
	defaults := yagocrawlcontract.DefaultFormatToggles()
	if !defaults.Text || !defaults.XMLFeeds || !defaults.PDF || !defaults.Office ||
		!defaults.Images || !defaults.Audio || !defaults.Misc {
		t.Fatalf("defaults must enable every family: %+v", defaults)
	}
	if defaults.Archives {
		t.Fatal("archives must default off for safety")
	}
}

func TestURLExtensionAndMime(t *testing.T) {
	if got := urlExtension("https://a.example/x/report.PDF?d=1#f"); got != "pdf" {
		t.Fatalf("extension = %q", got)
	}
	if got := mimeType("Application/PDF; charset=x"); got != "application/pdf" {
		t.Fatalf("mime = %q", got)
	}
}

func TestParseTextFamilyPlainMembers(t *testing.T) {
	toggles := yagocrawlcontract.DefaultFormatToggles()
	page, parsed := Parse(
		"https://a.example/notes.txt", "text/plain",
		[]byte("First line title\nsecond line body"), toggles,
	)
	if !parsed || page.Title != "First line title" || page.Text == "" {
		t.Fatalf("txt parse = %v %+v", parsed, page)
	}

	if rtfPage, parsed := Parse(
		"https://a.example/doc.rtf", "application/rtf", []byte(`{\rtf1 x}`), toggles,
	); !parsed || rtfPage.Text != "x" {
		t.Fatalf("rtf parse = %v %+v", parsed, rtfPage)
	}
	if _, parsed := Parse(
		"https://a.example/empty.txt", "text/plain", []byte("   "), toggles,
	); parsed {
		t.Fatal("blank text must not index")
	}

	long := make([]byte, 0, 200)
	for i := 0; i < 100; i++ {
		long = append(long, 'a', 'b')
	}
	page, parsed = Parse("https://a.example/long.tex", "text/plain", long, toggles)
	if !parsed || len([]rune(page.Title)) != textTitleRuneCap {
		t.Fatalf("long title not capped: %d", len([]rune(page.Title)))
	}
}

func TestEveryFamilyToggleControlsItsFamily(t *testing.T) {
	all := yagocrawlcontract.FormatToggles{
		Text: true, XMLFeeds: true, PDF: true, Office: true,
		Images: true, Audio: true, Misc: true, Archives: true,
	}
	for _, entry := range families() {
		if !entry.enabled(all) {
			t.Fatalf("family %q not enabled by its toggle", entry.name)
		}
		if entry.enabled(yagocrawlcontract.FormatToggles{}) {
			t.Fatalf("family %q enabled with all toggles off", entry.name)
		}
	}
}
