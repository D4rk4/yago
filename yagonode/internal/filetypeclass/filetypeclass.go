// Package filetypeclass classifies a crawled document's file type from its
// Content-Type and URL, so the filetype: search operator and the filetype
// navigation facet recognise a document by what it actually is rather than only
// by a file extension in its URL. Many documents are served at extension-less
// URLs — an arxiv PDF at /pdf/2401.12345, a CMS article at /about — where the
// URL alone yields no usable type, but the stored Content-Type does.
package filetypeclass

import (
	"mime"
	"path"
	"strings"
)

// mimeTokens maps a document Content-Type onto the canonical file-type token the
// filetype: operator and facet use (YaCy's url_file_ext convention). It covers
// the crawler's parser families (formatparse) plus the common media and
// application types a crawl meets.
var mimeTokens = map[string]string{
	"text/html":              "html",
	"application/xhtml+xml":  "html",
	"application/pdf":        "pdf",
	"application/postscript": "ps",
	"text/plain":             "txt",
	"text/csv":               "csv",
	"application/rtf":        "rtf",
	"text/rtf":               "rtf",
	"text/xml":               "xml",
	"application/xml":        "xml",
	"application/rss+xml":    "rss",
	"application/atom+xml":   "atom",
	"application/msword":     "doc",
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document": "docx",
	"application/vnd.ms-excel": "xls",
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":         "xlsx",
	"application/vnd.ms-powerpoint":                                             "ppt",
	"application/vnd.openxmlformats-officedocument.presentationml.presentation": "pptx",
	"application/vnd.oasis.opendocument.text":                                   "odt",
	"application/vnd.oasis.opendocument.spreadsheet":                            "ods",
	"application/vnd.oasis.opendocument.presentation":                           "odp",
	"image/jpeg":               "jpg",
	"image/png":                "png",
	"image/gif":                "gif",
	"image/bmp":                "bmp",
	"image/tiff":               "tiff",
	"image/svg+xml":            "svg",
	"image/webp":               "webp",
	"audio/mpeg":               "mp3",
	"audio/ogg":                "ogg",
	"audio/wav":                "wav",
	"audio/x-wav":              "wav",
	"audio/flac":               "flac",
	"video/mp4":                "mp4",
	"video/webm":               "webm",
	"video/x-msvideo":          "avi",
	"video/quicktime":          "mov",
	"video/mpeg":               "mpeg",
	"text/vcard":               "vcf",
	"text/x-vcard":             "vcf",
	"application/x-bittorrent": "torrent",
	"application/vnd.android.package-archive": "apk",
	"application/zip":                         "zip",
	"application/gzip":                        "gz",
	"application/x-tar":                       "tar",
	"application/java-archive":                "jar",
	"application/epub+zip":                    "epub",
}

// aliasCanon collapses interchangeable extension spellings onto one
// representative, so filetype:jpeg matches an image/jpeg document classified
// "jpg" and filetype:htm matches an HTML page.
var aliasCanon = map[string]string{
	"jpeg": "jpg",
	"jpe":  "jpg",
	"htm":  "html",
	"tif":  "tiff",
	"mpg":  "mpeg",
	"text": "txt",
}

// aliasKey folds a token onto its alias representative for comparison.
func aliasKey(token string) string {
	if canon, ok := aliasCanon[token]; ok {
		return canon
	}

	return token
}

// mimeToken resolves a Content-Type to its file-type token, or "" when the type
// is unknown or unset. The media type is parsed to drop any charset parameter.
func mimeToken(contentType string) string {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType, _, _ = strings.Cut(contentType, ";")
	}

	return mimeTokens[strings.ToLower(strings.TrimSpace(mediaType))]
}

// urlToken returns the lowercased path extension of a URL, or "" when there is
// none or it is implausibly long (a bare id, not a real extension).
func urlToken(rawURL string) string {
	trimmed := rawURL
	if index := strings.IndexAny(trimmed, "?#"); index >= 0 {
		trimmed = trimmed[:index]
	}
	ext := strings.TrimPrefix(strings.ToLower(path.Ext(trimmed)), ".")
	if len(ext) > 5 {
		return ""
	}

	return ext
}

// Canonical returns the single file-type token that classifies a document for
// the navigation facet: the Content-Type mapping when it names a known format,
// otherwise the URL path extension.
func Canonical(rawURL, contentType string) string {
	if token := mimeToken(contentType); token != "" {
		return token
	}

	return urlToken(rawURL)
}

// Matches reports whether a document with the given URL and Content-Type
// satisfies a filetype:<wanted> query. It is true when wanted matches the
// content-type token or the URL extension (aliases folded), so both a foo.pdf
// URL and an extension-less application/pdf document answer filetype:pdf.
func Matches(rawURL, contentType, wanted string) bool {
	want := aliasKey(strings.TrimPrefix(strings.ToLower(strings.TrimSpace(wanted)), "."))
	if want == "" {
		return false
	}
	if token := mimeToken(contentType); token != "" && aliasKey(token) == want {
		return true
	}
	if token := urlToken(rawURL); token != "" && aliasKey(token) == want {
		return true
	}

	return false
}
