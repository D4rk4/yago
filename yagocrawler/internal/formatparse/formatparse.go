// Package formatparse dispatches fetched documents to format parsers by content
// type and URL extension, the way YaCy's TextParser routes its parser registry.
// HTML/web text is the always-on core; the other format families switch on the
// operator's shared format toggles carried in every crawl profile. A family
// that is toggled on but has no parser yet reports the document as unparsed, so
// behavior stays honest while the families fill in.
package formatparse

import (
	"path"
	"strings"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawler/internal/pageparse"
)

// ParseFunc turns one fetched body into a parsed page; ok reports whether the
// body was actually parseable (a family may cover extensions it cannot parse
// yet).
type ParseFunc func(rawURL, contentType string, body []byte) (pageparse.ParsedPage, bool)

// family is one format family: its matching rules and, once implemented, its
// parser. Families without a parser match but do not parse.
type family struct {
	name       string
	extensions map[string]bool
	mimes      map[string]bool
	parse      ParseFunc
	enabled    func(yagocrawlcontract.FormatToggles) bool
}

// htmlExtensions match the always-on web-text core (YaCy: html, htm, shtml,
// xhtml, php, asp, aspx, cfm and the bare-path default).
var htmlExtensions = map[string]bool{
	"html": true, "htm": true, "shtml": true, "xhtml": true,
	"php": true, "asp": true, "aspx": true, "cfm": true, "": true,
}

var htmlMimes = map[string]bool{
	"text/html": true, "application/xhtml+xml": true,
}

// families lists the YaCy TextParser format families and their toggles; parse
// functions arrive with the per-family follow-up slices.
func families() []family {
	return []family{
		{
			name:       "text",
			extensions: set("txt", "tex", "csv", "rtf", "msg"),
			mimes:      set("text/plain", "text/csv", "application/rtf"),
			parse:      parseTextFamily,
			enabled:    func(t yagocrawlcontract.FormatToggles) bool { return t.Text },
		},
		{
			name:       "xmlfeeds",
			extensions: set("xml", "rss", "atom"),
			mimes: set(
				"text/xml", "application/xml", "application/rss+xml",
				"application/atom+xml",
			),
			enabled: func(t yagocrawlcontract.FormatToggles) bool { return t.XMLFeeds },
		},
		{
			name:       "pdf",
			extensions: set("pdf", "ps"),
			mimes:      set("application/pdf", "application/postscript"),
			enabled:    func(t yagocrawlcontract.FormatToggles) bool { return t.PDF },
		},
		{
			name: "office",
			extensions: set(
				"doc", "xls", "xla", "ppt", "pps",
				"docx", "dotx", "pptx", "ppsx", "potx", "xlsx", "xltx",
				"odt", "ods", "odp", "odg", "odc", "odf", "odb", "odi", "odm",
				"ott", "ots", "otp", "otg", "sxw", "sxc",
				"vsd", "vss", "vst", "mm",
			),
			mimes:   set("application/msword", "application/vnd.oasis.opendocument.text"),
			enabled: func(t yagocrawlcontract.FormatToggles) bool { return t.Office },
		},
		{
			name: "images",
			extensions: set(
				"jpg", "jpeg", "jpe", "png", "gif", "bmp", "wbmp",
				"tif", "tiff", "psd", "svg",
			),
			mimes: set(
				"image/jpeg", "image/png", "image/gif", "image/bmp",
				"image/tiff", "image/svg+xml",
			),
			enabled: func(t yagocrawlcontract.FormatToggles) bool { return t.Images },
		},
		{
			name: "audio",
			extensions: set(
				"mp3", "ogg", "wma", "wav", "m4a", "m4b", "m4p", "mp4",
				"aif", "aifc", "aiff", "ra", "rm", "sid",
			),
			mimes:   set("audio/mpeg", "audio/ogg", "audio/wav", "video/mp4"),
			enabled: func(t yagocrawlcontract.FormatToggles) bool { return t.Audio },
		},
		{
			name:       "misc",
			extensions: set("vcf", "torrent", "apk"),
			mimes:      set("text/vcard", "application/x-bittorrent"),
			enabled:    func(t yagocrawlcontract.FormatToggles) bool { return t.Misc },
		},
		{
			name: "archives",
			extensions: set(
				"zip", "jar", "epub", "tar", "gz", "tgz",
				"bz2", "tbz", "tbz2", "xz", "txz",
			),
			mimes:   set("application/zip", "application/gzip", "application/x-tar"),
			enabled: func(t yagocrawlcontract.FormatToggles) bool { return t.Archives },
		},
	}
}

// Parse dispatches the fetched body. The bool reports whether a parser
// produced an indexable page: HTML always parses; other families parse only
// when their toggle is on and their parser is implemented.
func Parse(
	rawURL, contentType string,
	body []byte,
	toggles yagocrawlcontract.FormatToggles,
) (pageparse.ParsedPage, bool) {
	ext := urlExtension(rawURL)
	mime := mimeType(contentType)
	if htmlMimes[mime] || (mime == "" || strings.HasPrefix(mime, "text/")) && htmlExtensions[ext] {
		return pageparse.ParseHTML(rawURL, contentType, body), true
	}

	for _, entry := range families() {
		if !entry.extensions[ext] && !entry.mimes[mime] {
			continue
		}
		if !entry.enabled(toggles) || entry.parse == nil {
			return pageparse.ParsedPage{URL: rawURL}, false
		}

		return entry.parse(rawURL, contentType, body)
	}

	// Unknown types fall back to the HTML parser, which degrades to link-less
	// text handling — the pre-registry behavior.
	return pageparse.ParseHTML(rawURL, contentType, body), true
}

func urlExtension(rawURL string) string {
	trimmed := rawURL
	if index := strings.IndexAny(trimmed, "?#"); index >= 0 {
		trimmed = trimmed[:index]
	}

	return strings.TrimPrefix(strings.ToLower(path.Ext(trimmed)), ".")
}

func mimeType(contentType string) string {
	mime, _, _ := strings.Cut(contentType, ";")

	return strings.ToLower(strings.TrimSpace(mime))
}

func set(values ...string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, value := range values {
		out[value] = true
	}

	return out
}
