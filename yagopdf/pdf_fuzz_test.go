package yagopdf

import (
	"os"
	"path/filepath"
	"testing"
)

func pdfFuzzSeeds() [][]byte {
	toUnicode := syntheticCMap(
		bfcharSection([]string{"<0001> <0041>", "<0002> <D83DDE00>"}),
		bfrangeSection([]string{"<0020> <007E> <0020>", "<0100> <0110> [<0061> <0062>]"}),
	)

	return [][]byte{
		[]byte("%PDF-1.7\n"),
		singlePagePDF(
			"BT /F1 12 Tf 72 700 Td (Fuzz seed page words) Tj T* [(Second) -250 (part)] TJ ET",
		),
		singlePagePDF(
			"BT <FEFF00460075007A007A> Tj (\\101\\102 \\(nested\\)) ' 1 2 (quoted) \" ET",
		),
		newSyntheticPDF().
			objectStream(10, []int{1, 2}, []string{"<< /Type /Page /Contents 3 0 R >>", "<< /Title (Packed title) >>"}).
			flateStream(3, "", "BT (Packed member words) Tj ET").
			trailer("<< /Info 2 0 R >>").
			finished(),
		toUnicodeFontPDF(toUnicode, "BT /F1 12 Tf <0001000200480069> Tj ET"),
		simpleEncodingFontPDF(
			"<< /BaseEncoding /WinAnsiEncoding /Differences [65 /B /C 128 /bullet] >>",
			"BT /F1 12 Tf (ABC\\200 differences words) Tj ET",
			func(*syntheticPDF) {},
		),
		newSyntheticPDF().
			page(1, "/Resources << /XObject << /Fm1 3 0 R >> >> /Contents 2 0 R").
			rawStream(2, "", "q /Fm1 Do Q").
			rawStream(3, "/Type /XObject /Subtype /Form /Resources << /XObject << /Fm1 3 0 R >> >>",
				"BT (Form words) Tj ET /Fm1 Do").
			finished(),
		newSyntheticPDF().
			page(1, "/Contents [2 0 R 3 0 R 4 0 R]").
			encodedStream(2, "/Filter [/ASCII85Decode /FlateDecode]",
				syntheticASCII85(syntheticZlib([]byte("BT (Chained filter words) Tj ET")))).
			encodedStream(3, "/Filter /LZWDecode", syntheticLZW([]byte("BT (Lzw filter words) Tj ET"))).
			encodedStream(4, "/Filter /ASCIIHexDecode", syntheticASCIIHex([]byte("BT (Hex filter words) Tj ET"))).
			finished(),
		newSyntheticPDF().
			page(1, "/Contents [2 0 R 3 0 R]").
			rawStream(2, "", "BT (Legitimate words before a bomb) Tj ET").
			encodedStream(3, "/Filter /FlateDecode", syntheticZlib(make([]byte, 1<<20))).
			finished(),
		newSyntheticPDF().
			page(1, "/Parent 5 0 R /Contents 2 0 R").
			indirectObject(5, "<< /Type /Pages /Kids [1 0 R] /Parent 5 0 R /Resources << >> >>").
			rawStream(2, "", "BT (Cyclic tree words) Tj ET").
			finished(),
	}
}

func FuzzReadPDF(f *testing.F) {
	for _, seed := range pdfFuzzSeeds() {
		f.Add(seed)
	}
	for _, directory := range []string{"encrypted", "producers"} {
		paths, err := filepath.Glob(filepath.Join("testdata", directory, "*.pdf"))
		if err != nil || len(paths) == 0 {
			f.Fatalf("fixture discovery %s: %v", directory, err)
		}
		for _, path := range paths {
			seed, readError := os.ReadFile(path)
			if readError != nil {
				f.Fatal(readError)
			}
			f.Add(seed)
		}
	}
	f.Fuzz(func(t *testing.T, body []byte) {
		document, readable := ReadPDF(body)
		requireExtractionInvariants(t, document, readable)
	})
}

func FuzzReadPostScript(f *testing.F) {
	for _, seed := range []string{
		"%!",
		"%!PS-Adobe-3.0\n72 720 moveto (Fuzz postscript words) show\n",
		"%!PS\n(Nested \\(parenthesised\\) words) show (Second string) show\n",
		"%!PS\n(Octal \\101\\102 and escapes \\n\\t) show <48656C6C6F20776F726C64> show\n",
		"%!PS\n(unterminated string",
		"%!PS\n\x00\x01\x02\xff\xfe(binary prefix words) show\n",
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, body []byte) {
		document, readable := ReadPostScript(body)
		requireExtractionInvariants(t, document, readable)
	})
}
