package formatparse

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/bzip2"
	"compress/gzip"
	"io"
	"strings"
)

const (
	archiveMaxEntries    = 100
	archiveMaxEntryBytes = 8 << 20
	archiveMaxTotalBytes = 64 << 20
	archiveMaxTextBytes  = 1 << 20
)

// parseArchive unpacks a container and dispatches each inner file to the
// registered parsers, the YaCy convention. The family defaults off; even when
// enabled, extraction is bounded (entry count, per-entry and total bytes) and
// nested archives are not descended into, so archive bombs stop at one level.
// xz/txz need a decompressor dependency and stay unparsed until its ADR.
func parseArchive(rawURL, _ string, body []byte) (Document, bool) {
	entries := archiveEntries(rawURL, body)
	if len(entries) == 0 {
		return Document{URL: rawURL}, false
	}
	// Inner files parse with archives off: one level of nesting only.
	toggles := DefaultToggles()
	var text strings.Builder
	links := make([]string, 0, 8)
	for _, entry := range entries {
		page, parsed := Parse(rawURL+"!/"+entry.name, "", entry.data, toggles)
		if !parsed {
			text.WriteString(entry.name + "\n")

			continue
		}
		text.WriteString(entry.name + "\n")
		if page.Title != "" && page.Title != entry.name {
			text.WriteString(page.Title + "\n")
		}
		text.WriteString(page.Text + "\n")
		links = append(links, page.FollowableLinks...)
		if text.Len() > archiveMaxTextBytes {
			break
		}
	}
	extracted := strings.TrimSpace(text.String())
	if extracted == "" {
		return Document{URL: rawURL}, false
	}

	return Document{
		URL:             rawURL,
		Title:           imageTitle(rawURL),
		Text:            extracted,
		Links:           links,
		FollowableLinks: links,
	}, true
}

// archiveEntry is one bounded, extracted inner file.
type archiveEntry struct {
	name string
	data []byte
}

// archiveEntries lists the container's inner files by its extension.
func archiveEntries(rawURL string, body []byte) []archiveEntry {
	switch urlExtension(rawURL) {
	case "zip", "jar", "epub":
		return zipEntries(body)
	case "tar":
		return tarEntries(bytes.NewReader(body))
	case "gz", "tgz":
		reader, err := gzip.NewReader(bytes.NewReader(body))
		if err != nil {
			return nil
		}

		return compressedEntries(rawURL, reader)
	case "bz2", "tbz", "tbz2":
		return compressedEntries(rawURL, bzip2.NewReader(bytes.NewReader(body)))
	default:
		// xz/txz: no stdlib decompressor; a dependency needs an ADR first.
		return nil
	}
}

func zipEntries(body []byte) []archiveEntry {
	reader, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return nil
	}
	entries := make([]archiveEntry, 0, 16)
	total := 0
	for _, file := range reader.File {
		if len(entries) >= archiveMaxEntries || total >= archiveMaxTotalBytes {
			break
		}
		if strings.HasSuffix(file.Name, "/") {
			continue
		}
		data, err := readZipEntryBounded(file)
		if err != nil {
			continue
		}
		total += len(data)
		entries = append(entries, archiveEntry{name: file.Name, data: data})
	}

	return entries
}

func readZipEntryBounded(file *zip.File) ([]byte, error) {
	opened, err := file.Open()
	if err != nil {
		return nil, err //nolint:wrapcheck // caller skips the entry.
	}
	defer func() { _ = opened.Close() }()

	//nolint:wrapcheck // caller skips the entry.
	return io.ReadAll(io.LimitReader(opened, archiveMaxEntryBytes))
}

func tarEntries(reader io.Reader) []archiveEntry {
	archive := tar.NewReader(reader)
	entries := make([]archiveEntry, 0, 16)
	total := 0
	for len(entries) < archiveMaxEntries && total < archiveMaxTotalBytes {
		header, err := archive.Next()
		if err != nil {
			break
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		data, err := io.ReadAll(io.LimitReader(archive, archiveMaxEntryBytes))
		if err != nil {
			break
		}
		total += len(data)
		entries = append(entries, archiveEntry{name: header.Name, data: data})
	}

	return entries
}

// compressedEntries handles single-stream gz/bz2: a compressed tar unpacks as
// a tar, anything else is one inner file named after the archive.
func compressedEntries(rawURL string, reader io.Reader) []archiveEntry {
	data, err := io.ReadAll(io.LimitReader(reader, archiveMaxEntryBytes))
	if err != nil || len(data) == 0 {
		return nil
	}
	inner := strings.TrimSuffix(imageTitle(rawURL), "."+urlExtension(rawURL))
	if urlExtension(rawURL) == "tgz" || urlExtension(rawURL) == "tbz" ||
		urlExtension(rawURL) == "tbz2" || strings.HasSuffix(inner, ".tar") {
		return tarEntries(bytes.NewReader(data))
	}

	return []archiveEntry{{name: inner, data: data}}
}
