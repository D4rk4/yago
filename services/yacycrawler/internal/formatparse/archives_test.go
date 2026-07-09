package formatparse

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/hex"
	"strings"
	"testing"
)

func archiveToggles() Toggles {
	toggles := DefaultToggles()
	toggles.Archives = true

	return toggles
}

func tarBody(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	writer := tar.NewWriter(&buf)
	if err := writer.WriteHeader(&tar.Header{
		Name: "dir/", Typeflag: tar.TypeDir, Mode: 0o755,
	}); err != nil {
		t.Fatalf("dir header: %v", err)
	}
	for name, content := range files {
		if err := writer.WriteHeader(&tar.Header{
			Name: name, Mode: 0o644, Size: int64(len(content)),
		}); err != nil {
			t.Fatalf("header %s: %v", name, err)
		}
		if _, err := writer.Write([]byte(content)); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}

	return buf.Bytes()
}

func TestParseZipArchiveDispatchesInnerParsers(t *testing.T) {
	body := zipBody(t, map[string]string{
		"docs/readme.txt": "Inner readme text",
		"feed.rss": `<rss version="2.0"><channel><title>Zipped feed</title>` +
			`<item><title>Item</title><link>https://a.example/z1</link></item></channel></rss>`,
		"binary.bin": "\x00\x01\x02",
	})
	page, parsed := Parse(
		"https://a.example/bundle.zip", "application/zip", body, archiveToggles(),
	)
	if !parsed || page.Title != "bundle.zip" {
		t.Fatalf("zip parse = %v %+v", parsed, page)
	}
	for _, want := range []string{
		"docs/readme.txt", "Inner readme text", "Zipped feed", "binary.bin",
	} {
		if !strings.Contains(page.Text, want) {
			t.Fatalf("zip missing %q in %q", want, page.Text)
		}
	}
	if len(page.FollowableLinks) != 1 || page.FollowableLinks[0] != "https://a.example/z1" {
		t.Fatalf("zip links = %v", page.FollowableLinks)
	}

	if _, parsed := Parse(
		"https://a.example/bundle.zip", "application/zip", body,
		DefaultToggles(),
	); parsed {
		t.Fatal("archives default off")
	}
}

func TestParseNestedArchiveStopsAtOneLevel(t *testing.T) {
	inner := zipBody(t, map[string]string{"deep.txt": "Deep text"})
	outer := zipBody(t, map[string]string{
		"nested.zip": string(inner),
		"top.txt":    "Top text",
	})
	page, parsed := Parse(
		"https://a.example/outer.zip", "application/zip", outer, archiveToggles(),
	)
	if !parsed || !strings.Contains(page.Text, "Top text") {
		t.Fatalf("outer parse = %v %+v", parsed, page)
	}
	if strings.Contains(page.Text, "Deep text") {
		t.Fatal("nested archive content must not extract")
	}
}

func TestParseTarGzipBzip2(t *testing.T) {
	files := map[string]string{"notes/a.txt": "Tar note text"}
	tarData := tarBody(t, files)

	page, parsed := Parse(
		"https://a.example/data.tar", "application/x-tar", tarData, archiveToggles(),
	)
	if !parsed || !strings.Contains(page.Text, "Tar note text") {
		t.Fatalf("tar parse = %v %+v", parsed, page)
	}

	var gz bytes.Buffer
	gzWriter := gzip.NewWriter(&gz)
	if _, err := gzWriter.Write(tarData); err != nil {
		t.Fatalf("gzip: %v", err)
	}
	if err := gzWriter.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	page, parsed = Parse(
		"https://a.example/data.tgz", "application/gzip", gz.Bytes(), archiveToggles(),
	)
	if !parsed || !strings.Contains(page.Text, "Tar note text") {
		t.Fatalf("tgz parse = %v %+v", parsed, page)
	}

	var plain bytes.Buffer
	plainWriter := gzip.NewWriter(&plain)
	if _, err := plainWriter.Write([]byte("Plain gzipped text body")); err != nil {
		t.Fatalf("gzip plain: %v", err)
	}
	if err := plainWriter.Close(); err != nil {
		t.Fatalf("gzip plain close: %v", err)
	}
	page, parsed = Parse(
		"https://a.example/note.txt.gz", "application/gzip", plain.Bytes(), archiveToggles(),
	)
	if !parsed || !strings.Contains(page.Text, "Plain gzipped text body") {
		t.Fatalf("gz parse = %v %+v", parsed, page)
	}
}

func TestArchiveEdges(t *testing.T) {
	toggles := archiveToggles()
	if _, parsed := Parse(
		"https://a.example/broken.zip", "application/zip", []byte("nope"), toggles,
	); parsed {
		t.Fatal("broken zip must stay unparsed")
	}
	if _, parsed := Parse(
		"https://a.example/data.xz", "application/x-xz", []byte("\xfd7zXZ"), toggles,
	); parsed {
		t.Fatal("xz must stay unparsed until its decompressor ADR")
	}
	if _, parsed := Parse(
		"https://a.example/broken.gz", "application/gzip", []byte("nope"), toggles,
	); parsed {
		t.Fatal("broken gzip must stay unparsed")
	}
	if entries := tarEntries(bytes.NewReader([]byte("not a tar"))); len(entries) != 0 {
		t.Fatalf("bad tar entries = %v", entries)
	}
	if entries := zipEntries(zipBody(t, map[string]string{})); len(entries) != 0 {
		t.Fatalf("empty zip entries = %v", entries)
	}
	empty := zipBody(t, map[string]string{})
	if _, parsed := Parse(
		"https://a.example/empty.zip",
		"application/zip",
		empty,
		toggles,
	); parsed {
		t.Fatal("empty archive must stay unparsed")
	}
}

func TestArchiveBoundsAndTitles(t *testing.T) {
	// Inner page titles differing from the entry name index too (RSS title).
	feed := `<rss version="2.0"><channel><title>Named feed</title>` +
		`<item><title>I</title></item></channel></rss>`
	body := zipBody(t, map[string]string{"f.rss": feed})
	page, parsed := Parse("https://a.example/t.zip", "application/zip", body, archiveToggles())
	if !parsed || !strings.Contains(page.Text, "Named feed") {
		t.Fatalf("titled inner = %v %+v", parsed, page)
	}

	// Entry-count cap: more files than the limit stops the walk.
	many := map[string]string{}
	for i := 0; i < archiveMaxEntries+20; i++ {
		many[strings.Repeat("d", 2)+"/"+string(rune('a'+i%26))+string(rune('a'+(i/26)%26))+".txt"] = "t"
	}
	if entries := zipEntries(zipBody(t, many)); len(entries) != archiveMaxEntries {
		t.Fatalf("zip cap = %d", len(entries))
	}

	// The extracted-text cap stops appending further entries.
	big := map[string]string{
		"a.txt": strings.Repeat("x", archiveMaxTextBytes+100),
		"b.txt": "after cap",
	}
	page, parsed = Parse(
		"https://a.example/big.zip",
		"application/zip",
		zipBody(t, big),
		archiveToggles(),
	)
	if !parsed {
		t.Fatal("big archive must parse")
	}

	// A corrupt zip entry is skipped.
	corruptSrc := zipBody(t, map[string]string{"only.txt": "text"})
	corrupt := bytes.Replace(corruptSrc, []byte("PK\x03\x04"), []byte("XXXX"), 1)
	if entries := zipEntries(corrupt); len(entries) != 0 {
		t.Fatalf("corrupt zip entries = %v", entries)
	}

	// A tar with an oversize declared entry stops reading.
	var buf bytes.Buffer
	writer := tar.NewWriter(&buf)
	_ = writer.WriteHeader(&tar.Header{Name: "big.bin", Mode: 0o644, Size: 100})
	_, _ = writer.Write(bytes.Repeat([]byte("y"), 50))
	// Close is skipped so the tar stream truncates mid-entry.
	if entries := tarEntries(bytes.NewReader(buf.Bytes())); len(entries) != 0 {
		t.Fatalf("truncated tar = %v", entries)
	}

	// A .tar.gz (not .tgz) routes into the tar reader via its inner name.
	var gz bytes.Buffer
	gzWriter := gzip.NewWriter(&gz)
	_, _ = gzWriter.Write(tarBody(t, map[string]string{"in.txt": "Tar in gz"}))
	_ = gzWriter.Close()
	page, parsed = Parse(
		"https://a.example/data.tar.gz",
		"application/gzip",
		gz.Bytes(),
		archiveToggles(),
	)
	if !parsed || !strings.Contains(page.Text, "Tar in gz") {
		t.Fatalf("tar.gz = %v %+v", parsed, page)
	}
}

func TestArchiveLastBranches(t *testing.T) {
	// bzip2 decompresses through the stdlib reader.
	const bz2Hex = "425a6839314159265359e2230f670000019580400010000e61d4502000222699a989ea7a8400002d221c916e173dda32935c2f8bb9229c2848711187b380"
	raw, err := hex.DecodeString(bz2Hex)
	if err != nil {
		t.Fatalf("hex: %v", err)
	}
	page, parsed := Parse(
		"https://a.example/note.txt.bz2",
		"application/x-bzip2",
		raw,
		archiveToggles(),
	)
	if !parsed || !strings.Contains(page.Text, "Bzipped text content here") {
		t.Fatalf("bz2 = %v %+v", parsed, page)
	}

	// Directory entries in a zip skip.
	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	if _, err := writer.Create("dir/"); err != nil {
		t.Fatalf("dir: %v", err)
	}
	entry, err := writer.Create("dir/file.txt")
	if err != nil {
		t.Fatalf("file: %v", err)
	}
	if _, err := entry.Write([]byte("In dir")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	entries := zipEntries(buf.Bytes())
	if len(entries) != 1 || entries[0].name != "dir/file.txt" {
		t.Fatalf("dir skip = %+v", entries)
	}

	// An empty gzip stream yields no entries.
	var empty bytes.Buffer
	gzWriter := gzip.NewWriter(&empty)
	_ = gzWriter.Close()
	if _, parsed := Parse(
		"https://a.example/void.gz", "application/gzip", empty.Bytes(), archiveToggles(),
	); parsed {
		t.Fatal("empty gzip must stay unparsed")
	}

	// Entries whose names are empty and bodies unparsable leave no text.
	blank := []archiveEntry{{name: "", data: nil}}
	_ = blank
	var noText bytes.Buffer
	noTextWriter := zip.NewWriter(&noText)
	blankEntry, err := noTextWriter.CreateHeader(&zip.FileHeader{Name: " "})
	if err != nil {
		t.Fatalf("blank header: %v", err)
	}
	if _, err := blankEntry.Write([]byte{}); err != nil {
		t.Fatalf("blank write: %v", err)
	}
	if err := noTextWriter.Close(); err != nil {
		t.Fatalf("blank close: %v", err)
	}
	if _, parsed := Parse(
		"https://a.example/blank.zip", "application/zip", noText.Bytes(), archiveToggles(),
	); parsed {
		t.Fatal("archive with only a blank-named empty entry must stay unparsed")
	}
}
