package formatparse

import (
	"archive/zip"
	"bytes"
	"fmt"
	"strings"

	"github.com/D4rk4/yago/yago-crawler/internal/pageparse"
)

// parseMisc handles the misc family: vCard contacts, BitTorrent metadata, and
// Android packages.
func parseMisc(rawURL, contentType string, body []byte) (pageparse.ParsedPage, bool) {
	switch urlExtension(rawURL) {
	case "vcf":
		return parseVCard(rawURL, body)
	case "torrent":
		return parseTorrent(rawURL, body)
	case "apk":
		return parseAPK(rawURL, body)
	}

	return parseMiscByContent(rawURL, contentType, body)
}

// parseMiscByContent resolves a misc document served at an extension-less URL
// from its Content-Type. A vCard also announces itself in its opening line, so
// its text magic is a fallback; the binary torrent and APK formats are left to
// their Content-Type, since their magic (bencode, a zip container) overlaps
// other families.
func parseMiscByContent(rawURL, contentType string, body []byte) (pageparse.ParsedPage, bool) {
	switch mimeType(contentType) {
	case "text/vcard", "text/x-vcard":
		return parseVCard(rawURL, body)
	case "application/x-bittorrent":
		return parseTorrent(rawURL, body)
	case "application/vnd.android.package-archive":
		return parseAPK(rawURL, body)
	}
	if bytes.Contains(clipHead(body), []byte("BEGIN:VCARD")) {
		return parseVCard(rawURL, body)
	}

	return pageparse.ParsedPage{URL: rawURL}, false
}

// vCardProperties lists the properties worth indexing, in output order.
var vCardProperties = []string{
	"FN", "N", "ORG", "TITLE", "TEL", "EMAIL", "ADR", "URL", "NOTE", "NICKNAME",
}

// parseVCard indexes the contact fields of RFC 6350 vCards; multiple cards in
// one file all index.
func parseVCard(rawURL string, body []byte) (pageparse.ParsedPage, bool) {
	var text strings.Builder
	title := ""
	for _, line := range strings.Split(unfoldVCard(string(body)), "\n") {
		name, value := vCardProperty(line)
		if name == "" || value == "" {
			continue
		}
		if name == "FN" && title == "" {
			title = value
		}
		text.WriteString(name + ": " + value + "\n")
	}
	extracted := strings.TrimSpace(text.String())
	if extracted == "" {
		return pageparse.ParsedPage{URL: rawURL}, false
	}
	if title == "" {
		title = textTitle(extracted)
	}

	return pageparse.ParsedPage{URL: rawURL, Title: title, Text: extracted}, true
}

// unfoldVCard joins RFC 6350 folded continuation lines (leading space or tab).
func unfoldVCard(raw string) string {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	raw = strings.ReplaceAll(raw, "\n ", "")

	return strings.ReplaceAll(raw, "\n\t", "")
}

// vCardProperty splits one content line into an indexable property name and
// value; group prefixes and parameters are dropped.
func vCardProperty(line string) (string, string) {
	name, value, found := strings.Cut(line, ":")
	if !found {
		return "", ""
	}
	name, _, _ = strings.Cut(name, ";")
	if _, group, grouped := strings.Cut(name, "."); grouped {
		name = group
	}
	name = strings.ToUpper(strings.TrimSpace(name))
	for _, allowed := range vCardProperties {
		if name == allowed {
			return name, strings.TrimSpace(strings.ReplaceAll(value, "\\n", " "))
		}
	}

	return "", ""
}

// parseTorrent indexes a BitTorrent metainfo file: name, comment, tracker,
// and the contained file list.
func parseTorrent(rawURL string, body []byte) (pageparse.ParsedPage, bool) {
	decoded, _, err := decodeBencode(body, 0)
	if err != nil {
		return pageparse.ParsedPage{URL: rawURL}, false
	}
	root, ok := decoded.(map[string]any)
	if !ok {
		return pageparse.ParsedPage{URL: rawURL}, false
	}
	var text strings.Builder
	title := ""
	info, _ := root["info"].(map[string]any)
	if info != nil {
		if name, ok := info["name"].(string); ok {
			title = name
			text.WriteString("Name: " + name + "\n")
		}
	}
	if comment, ok := root["comment"].(string); ok && comment != "" {
		text.WriteString("Comment: " + comment + "\n")
	}
	if announce, ok := root["announce"].(string); ok && announce != "" {
		text.WriteString("Tracker: " + announce + "\n")
	}
	appendTorrentFiles(&text, info)
	extracted := strings.TrimSpace(text.String())
	if extracted == "" {
		return pageparse.ParsedPage{URL: rawURL}, false
	}
	if title == "" {
		title = textTitle(extracted)
	}

	return pageparse.ParsedPage{URL: rawURL, Title: title, Text: extracted}, true
}

// appendTorrentFiles lists multi-file torrent paths and sizes.
func appendTorrentFiles(text *strings.Builder, info map[string]any) {
	if info == nil {
		return
	}
	if length, ok := info["length"].(int64); ok {
		fmt.Fprintf(text, "Size: %d bytes\n", length)
	}
	files, _ := info["files"].([]any)
	for _, entry := range files {
		file, _ := entry.(map[string]any)
		if file == nil {
			continue
		}
		segments, _ := file["path"].([]any)
		parts := make([]string, 0, len(segments))
		for _, segment := range segments {
			if part, ok := segment.(string); ok {
				parts = append(parts, part)
			}
		}
		if len(parts) > 0 {
			text.WriteString("File: " + strings.Join(parts, "/") + "\n")
		}
	}
}

const apkMaxListedFiles = 200

// parseAPK indexes an Android package as its zip file list plus the package
// hints readable without decoding the binary AndroidManifest (a documented
// best-effort divergence from YaCy's full manifest decode).
func parseAPK(rawURL string, body []byte) (pageparse.ParsedPage, bool) {
	reader, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return pageparse.ParsedPage{URL: rawURL}, false
	}
	var text strings.Builder
	for i, file := range reader.File {
		if i >= apkMaxListedFiles {
			break
		}
		text.WriteString("File: " + file.Name + "\n")
	}
	if manifest := apkManifestStrings(reader); manifest != "" {
		text.WriteString(manifest)
	}
	extracted := strings.TrimSpace(text.String())
	if extracted == "" {
		return pageparse.ParsedPage{URL: rawURL}, false
	}

	return pageparse.ParsedPage{
		URL:   rawURL,
		Title: imageTitle(rawURL),
		Text:  extracted,
	}, true
}

// apkManifestStrings extracts readable UTF-16LE runs from the binary
// AndroidManifest — package names and permissions appear in its string pool.
func apkManifestStrings(reader *zip.Reader) string {
	for _, file := range reader.File {
		if file.Name != "AndroidManifest.xml" {
			continue
		}
		content, err := readZipPart(file)
		if err != nil {
			return ""
		}
		var text strings.Builder
		for _, run := range printableRuns(content) {
			if strings.Contains(run, ".") || strings.Contains(run, "permission") {
				text.WriteString("Manifest: " + run + "\n")
			}
		}

		return text.String()
	}

	return ""
}
