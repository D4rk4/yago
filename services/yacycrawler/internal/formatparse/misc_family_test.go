package formatparse

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
)

func TestParseVCard(t *testing.T) {
	card := "BEGIN:VCARD\r\nVERSION:4.0\r\nFN:Erika Mustermann\r\n" +
		"item1.ORG:Example\r\n Corp\r\nTEL;TYPE=work:+49 123\r\n" +
		"NOTE:Line one\\nline two\r\nX-SKIP:hidden\r\nEND:VCARD\r\n"
	page, parsed := Parse(
		"https://a.example/contact.vcf", "text/vcard", []byte(card),
		DefaultToggles(),
	)
	if !parsed || page.Title != "Erika Mustermann" {
		t.Fatalf("vcf parse = %v %+v", parsed, page)
	}
	for _, want := range []string{
		"FN: Erika Mustermann", "ORG: ExampleCorp", "TEL: +49 123", "NOTE: Line one line two",
	} {
		if !strings.Contains(page.Text, want) {
			t.Fatalf("vcf missing %q in %q", want, page.Text)
		}
	}
	if strings.Contains(page.Text, "hidden") || strings.Contains(page.Text, "VCARD") {
		t.Fatalf("vcf leaked non-indexed properties: %q", page.Text)
	}

	if _, parsed := Parse(
		"https://a.example/empty.vcf", "text/vcard", []byte("BEGIN:VCARD\nEND:VCARD"),
		DefaultToggles(),
	); parsed {
		t.Fatal("field-free vcard must stay unparsed")
	}
}

func TestParseTorrent(t *testing.T) {
	torrent := "d8:announce32:https://tracker.example/announce7:comment9:Demo data4:infod5:filesld6:lengthi100e4:pathl4:docs6:a.htmleed6:lengthi50e4:pathl5:b.txteee4:name7:Bundle112:piece lengthi16384eee"
	page, parsed := Parse(
		"https://a.example/data.torrent", "application/x-bittorrent", []byte(torrent),
		DefaultToggles(),
	)
	if !parsed || page.Title != "Bundle1" {
		t.Fatalf("torrent parse = %v %+v", parsed, page)
	}
	for _, want := range []string{
		"Comment: Demo data", "Tracker: https://tracker.example/announce",
		"File: docs/a.html", "File: b.txt",
	} {
		if !strings.Contains(page.Text, want) {
			t.Fatalf("torrent missing %q in %q", want, page.Text)
		}
	}

	single := "d4:infod6:lengthi2048e4:name8:solo.isoee"
	page, parsed = Parse(
		"https://a.example/one.torrent", "application/x-bittorrent", []byte(single),
		DefaultToggles(),
	)
	if !parsed || !strings.Contains(page.Text, "Size: 2048 bytes") {
		t.Fatalf("single torrent = %v %+v", parsed, page)
	}

	for _, bad := range []string{"", "x", "d3:foo", "i12", "le", "d4:infoi1ee"} {
		if _, parsed := parseTorrent(
			"https://a.example/bad.torrent",
			[]byte(bad),
		); parsed &&
			bad != "d4:infoi1ee" {
			t.Fatalf("bad torrent %q parsed", bad)
		}
	}
}

func TestParseAPK(t *testing.T) {
	manifest := make([]byte, 0, 128)
	manifest = append(manifest, 0x03, 0x00, 0x08, 0x00)
	for _, b := range []byte("com.example.app") {
		manifest = append(manifest, b, 0x00)
	}
	manifest = append(manifest, 0x00, 0x00)
	for _, b := range []byte("android.permission.INTERNET") {
		manifest = append(manifest, b, 0x00)
	}
	body := zipBody(t, map[string]string{
		"AndroidManifest.xml": string(manifest),
		"classes.dex":         "dex",
		"res/layout/main.xml": "layout",
	})
	page, parsed := Parse(
		"https://a.example/tool.apk", "application/vnd.android.package-archive", body,
		DefaultToggles(),
	)
	if !parsed || page.Title != "tool.apk" {
		t.Fatalf("apk parse = %v %+v", parsed, page)
	}
	for _, want := range []string{
		"File: classes.dex", "File: res/layout/main.xml",
		"Manifest: com.example.app", "Manifest: android.permission.INTERNET",
	} {
		if !strings.Contains(page.Text, want) {
			t.Fatalf("apk missing %q in %q", want, page.Text)
		}
	}

	if _, parsed := Parse(
		"https://a.example/broken.apk", "application/zip", []byte("nope"),
		DefaultToggles(),
	); parsed {
		t.Fatal("broken apk must stay unparsed")
	}
	if _, parsed := parseMisc("https://a.example/other.bin", "", nil); parsed {
		t.Fatal("non-misc extension must stay unparsed")
	}
}

func TestBencodeEdges(t *testing.T) {
	if _, _, err := decodeBencode([]byte("i-42e"), 0); err != nil {
		t.Fatal("negative int must decode")
	}
	if _, _, err := decodeBencode([]byte("ixe"), 0); err == nil {
		t.Fatal("non-numeric int must fail")
	}
	if _, _, err := decodeBencode([]byte("9999:x"), 0); err == nil {
		t.Fatal("overlong string must fail")
	}
	if _, _, err := decodeBencode([]byte("l i1e"), 0); err == nil {
		t.Fatal("unterminated list element must fail")
	}
	if _, _, err := decodeBencode([]byte("li1e"), 0); err == nil {
		t.Fatal("unterminated list must fail")
	}
	if _, _, err := decodeBencode([]byte("d1:a"), 0); err == nil {
		t.Fatal("dict without value must fail")
	}
	if _, _, err := decodeBencode([]byte("d1:ai1e"), 0); err == nil {
		t.Fatal("unterminated dict must fail")
	}
	deep := strings.Repeat("l", bencodeMaxDepth+2)
	if _, _, err := decodeBencode([]byte(deep), 0); err == nil {
		t.Fatal("over-deep nesting must fail")
	}
}

func TestMiscRemainingBranches(t *testing.T) {
	// vCard without FN falls back to the first line as title.
	page, parsed := parseVCard("https://a.example/nofn.vcf", []byte("TEL:+1 555\n"))
	if !parsed || page.Title != "TEL: +1 555" {
		t.Fatalf("nofn vcard = %v %+v", parsed, page)
	}

	// Torrent without a name titles from its first line; junk file entries skip.
	torrent := "d7:comment4:Only4:infod5:filesli7eee4:pathl0:eee"
	tPage, tParsed := parseTorrent("https://a.example/noname.torrent", []byte(torrent))
	if !tParsed || !strings.HasPrefix(tPage.Title, "Comment: Only") {
		t.Fatalf("nameless torrent = %v %+v", tParsed, tPage)
	}

	// A bencode string without a colon fails.
	if _, _, err := decodeBencodeString([]byte("42")); err == nil {
		t.Fatal("colonless bencode string must fail")
	}
	// A dict with a non-string key fails.
	if _, _, err := decodeBencode([]byte("di1ei2ee"), 0); err == nil {
		t.Fatal("dict with integer key must fail")
	}

	// An APK with over-limit entries clips; an empty zip stays unparsed.
	many := map[string]string{}
	for i := 0; i < apkMaxListedFiles+10; i++ {
		many[strings.Repeat("f", 1)+"/"+strings.Repeat("x", 3)+string(rune('a'+i%26))+string(rune('a'+(i/26)%26))+".bin"] = "z"
	}
	page, parsed = parseAPK("https://a.example/big.apk", zipBody(t, many))
	if !parsed || strings.Count(page.Text, "File: ") != apkMaxListedFiles {
		t.Fatalf("apk clip = %v count=%d", parsed, strings.Count(page.Text, "File: "))
	}

	empty := zipBody(t, map[string]string{})
	if _, parsed := parseAPK("https://a.example/empty.apk", empty); parsed {
		t.Fatal("empty apk must stay unparsed")
	}

	// A manifest whose entry is corrupt yields no manifest lines.
	corruptZip := zipBody(t, map[string]string{"AndroidManifest.xml": "com.example.corrupt"})
	corrupt := bytes.Replace(corruptZip, []byte("PK\x03\x04"), []byte("XXXX"), 1)
	reader, err := zip.NewReader(bytes.NewReader(corrupt), int64(len(corrupt)))
	if err != nil {
		t.Fatalf("corrupt zip unreadable: %v", err)
	}
	if got := apkManifestStrings(reader); got != "" {
		t.Fatalf("corrupt manifest = %q", got)
	}
	plain := zipBody(t, map[string]string{"other.txt": "x"})
	plainReader, err := zip.NewReader(bytes.NewReader(plain), int64(len(plain)))
	if err != nil {
		t.Fatalf("plain zip: %v", err)
	}
	if got := apkManifestStrings(plainReader); got != "" {
		t.Fatalf("manifest-free apk strings = %q", got)
	}
}
