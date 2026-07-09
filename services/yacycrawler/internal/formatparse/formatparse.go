// Package formatparse extracts indexable text from non-HTML document formats —
// legacy Office (.doc/.xls/.ppt over OLE2), OOXML and OpenDocument, PDF and
// PostScript, RTF, feeds, audio tags, image metadata (EXIF, SVG), archives,
// and a few misc formats — dispatching by content type and URL extension the
// way YaCy's TextParser routes its parser registry. Everything is implemented
// on the standard library alone.
//
// HTML is deliberately out of scope: IsHTML tells the caller a body belongs to
// its own web-text extractor, and Parse reports such bodies (and unknown
// types) as unparsed. The format families switch on the caller's Toggles, so
// an operator chooses which families a crawl may index.
package formatparse

import (
	"path"
	"strings"
)

// ParseFunc turns one fetched body into a parsed page; ok reports whether the
// body was actually parseable (a family may cover extensions it cannot parse
// yet).
type ParseFunc func(rawURL, contentType string, body []byte) (Document, bool)

// family is one format family: its matching rules and, once implemented, its
// parser. Families without a parser match but do not parse.
type family struct {
	name       string
	extensions map[string]bool
	mimes      map[string]bool
	parse      ParseFunc
	enabled    func(Toggles) bool
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
			enabled:    func(t Toggles) bool { return t.Text },
		},
		{
			name:       "xmlfeeds",
			extensions: set("xml", "rss", "atom"),
			mimes: set(
				"text/xml", "application/xml", "application/rss+xml",
				"application/atom+xml",
			),
			parse:   parseXMLFeeds,
			enabled: func(t Toggles) bool { return t.XMLFeeds },
		},
		{
			name:       "pdf",
			extensions: set("pdf", "ps"),
			mimes:      set("application/pdf", "application/postscript"),
			parse:      parsePDF,
			enabled:    func(t Toggles) bool { return t.PDF },
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
			parse:   parseOffice,
			enabled: func(t Toggles) bool { return t.Office },
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
			parse:   parseImage,
			enabled: func(t Toggles) bool { return t.Images },
		},
		{
			name: "audio",
			extensions: set(
				"mp3", "ogg", "wma", "wav", "m4a", "m4b", "m4p", "mp4",
				"aif", "aifc", "aiff", "ra", "rm", "sid",
			),
			mimes:   set("audio/mpeg", "audio/ogg", "audio/wav", "video/mp4"),
			parse:   parseAudio,
			enabled: func(t Toggles) bool { return t.Audio },
		},
		{
			name:       "misc",
			extensions: set("vcf", "torrent", "apk"),
			mimes:      set("text/vcard", "application/x-bittorrent"),
			parse:      parseMisc,
			enabled:    func(t Toggles) bool { return t.Misc },
		},
		{
			name: "archives",
			extensions: set(
				"zip", "jar", "epub", "tar", "gz", "tgz",
				"bz2", "tbz", "tbz2", "xz", "txz",
			),
			mimes:   set("application/zip", "application/gzip", "application/x-tar"),
			parse:   parseArchive,
			enabled: func(t Toggles) bool { return t.Archives },
		},
	}
}

// dispatch runs the family parser when the family is enabled and implemented.
func (f family) dispatch(
	rawURL, contentType string,
	body []byte,
	toggles Toggles,
) (Document, bool) {
	if !f.enabled(toggles) || f.parse == nil {
		return Document{URL: rawURL}, false
	}

	return f.parse(rawURL, contentType, body)
}

// IsHTML reports whether a fetched body belongs to the caller's own web-text
// extractor rather than to one of the format families here: an HTML media
// type, or a text/unknown media type behind an HTML-ish URL extension.
func IsHTML(rawURL, contentType string) bool {
	mime := mimeType(contentType)
	if htmlMimes[mime] {
		return true
	}

	return (mime == "" || strings.HasPrefix(mime, "text/")) &&
		htmlExtensions[urlExtension(rawURL)]
}

// Parse dispatches the fetched body to its format family. The bool reports
// whether a parser produced an indexable document: a family parses only when
// its toggle is on and its parser is implemented, and HTML or unknown types
// report false so the caller routes them to its own web-text extractor.
func Parse(
	rawURL, contentType string,
	body []byte,
	toggles Toggles,
) (Document, bool) {
	if IsHTML(rawURL, contentType) {
		return Document{URL: rawURL}, false
	}

	// The URL extension is more specific than generic MIME bindings (a
	// FreeMind .mm arrives as text/xml), so extension matches win.
	ext := urlExtension(rawURL)
	mime := mimeType(contentType)
	registered := families()
	for _, entry := range registered {
		if entry.extensions[ext] {
			return entry.dispatch(rawURL, contentType, body, toggles)
		}
	}
	for _, entry := range registered {
		if entry.mimes[mime] {
			return entry.dispatch(rawURL, contentType, body, toggles)
		}
	}

	// Unknown types carry no family parser; the caller's web-text extractor
	// is the honest fallback.
	return Document{URL: rawURL}, false
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
