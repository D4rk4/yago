package yagopdf

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"io"
	"strings"
	"testing"
)

const (
	hostileLegitimatePageText = "Legitimate harbour survey precedes the hostile stream"
	hostileTextBombOperators  = "BT (bomb filler words) Tj ET\n"
)

func TestSyntheticRepeatedZlibInflatesToTheRequestedPattern(t *testing.T) {
	pattern := []byte("BT (pattern) Tj ET\n")
	compressed := syntheticRepeatedZlib(pattern, 3*hostileRepeatedChunkBytes)
	reader, err := zlib.NewReader(bytes.NewReader(compressed))
	if err != nil {
		t.Fatalf("zlib header: %v", err)
	}
	inflated, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("inflate with checksum: %v", err)
	}
	if len(inflated) < 3*hostileRepeatedChunkBytes-len(pattern)*3 ||
		len(inflated)%len(pattern) != 0 {
		t.Fatalf("inflated bytes = %d", len(inflated))
	}
	if !bytes.Equal(inflated, bytes.Repeat(pattern, len(inflated)/len(pattern))) {
		t.Fatal("inflated bytes are not the repeated pattern")
	}
	corrupted := bytes.Clone(compressed)
	corrupted[len(corrupted)-1] ^= 0xFF
	corruptedReader, err := zlib.NewReader(bytes.NewReader(corrupted))
	if err != nil {
		t.Fatalf("zlib header: %v", err)
	}
	if _, err := io.ReadAll(corruptedReader); err == nil {
		t.Fatal("a corrupted checksum must not inflate cleanly")
	}
	if bomb := hostileZeroBomb(); len(bomb) > 2<<20 {
		t.Fatalf("one gibibyte bomb occupies %d compressed bytes", len(bomb))
	}
}

func TestHostileFlateZeroBombInOneContentStreamStaysBounded(t *testing.T) {
	parallelWhenAllocationIsUnmeasured(t)
	body := newSyntheticPDF().
		page(1, "/Contents [2 0 R 3 0 R]").
		rawStream(2, "", "BT ("+hostileLegitimatePageText+") Tj ET").
		encodedStream(3, "/Filter /FlateDecode", hostileZeroBomb()).
		finished()
	extraction := requireBoundedExtraction(t, body)
	if !extraction.readable || extraction.document.Text != hostileLegitimatePageText {
		t.Fatalf("zero bomb extraction = %t %q", extraction.readable, extraction.document.Text)
	}
}

func TestHostileFlateTextOperatorBombStopsAtTheOutputCap(t *testing.T) {
	parallelWhenAllocationIsUnmeasured(t)
	body := newSyntheticPDF().
		page(1, "/Contents [2 0 R 3 0 R]").
		rawStream(2, "", "BT ("+hostileLegitimatePageText+") Tj ET").
		encodedStream(3, "/Filter /FlateDecode", hostileTextOperatorBomb()).
		finished()
	extraction := requireBoundedExtraction(t, body)
	if !extraction.readable ||
		!strings.HasPrefix(extraction.document.Text, hostileLegitimatePageText) {
		t.Fatalf("text bomb extraction = %t %q", extraction.readable,
			extraction.document.Text[:min(len(extraction.document.Text), 128)])
	}
	if len(extraction.document.Text) < pdfMaxTextBytes/2 {
		t.Fatalf("text bomb yielded %d bytes; the cap, not an early stop, must end it",
			len(extraction.document.Text))
	}
}

func TestHostileBombStreamsStopAtTheAggregateDecodeQuota(t *testing.T) {
	parallelWhenAllocationIsUnmeasured(t)
	nestedBomb := syntheticZlib(hostileZeroBomb())
	pdf := newSyntheticPDF()
	references := make([]int, 0, 301)
	references = append(references, 2)
	for reference := 3; reference < 303; reference++ {
		references = append(references, reference)
	}
	pdf.page(1, "/Contents "+contentsArray(references...)).
		rawStream(2, "", "BT ("+hostileLegitimatePageText+") Tj ET")
	for _, reference := range references[1:] {
		pdf.encodedStream(reference, "/Filter [/FlateDecode /FlateDecode]", nestedBomb)
	}
	extraction := requireBoundedExtraction(t, pdf.finished())
	if !extraction.readable || extraction.document.Text != hostileLegitimatePageText {
		t.Fatalf("aggregate bomb extraction = %t %q", extraction.readable, extraction.document.Text)
	}
}

func TestHostileNestedFilterChainBombsStayBounded(t *testing.T) {
	parallelWhenAllocationIsUnmeasured(t)
	zeroBomb := hostileZeroBomb()
	textBomb := hostileTextOperatorBomb()
	cases := []struct {
		name    string
		filters string
		encoded []byte
		capped  bool
	}{
		{
			"three flate levels of zeros", "[/FlateDecode /FlateDecode /FlateDecode]",
			syntheticZlib(syntheticZlib(zeroBomb)), false,
		},
		{
			"three flate levels of text operators", "[/FlateDecode /FlateDecode /FlateDecode]",
			syntheticZlib(syntheticZlib(textBomb)), true,
		},
		{
			"flate under ascii hex", "[/ASCIIHexDecode /FlateDecode]",
			syntheticASCIIHex(zeroBomb), false,
		},
		{
			"flate under ascii85", "[/ASCII85Decode /FlateDecode /FlateDecode]",
			syntheticASCII85(syntheticZlib(zeroBomb)), false,
		},
		{
			"ascii hex digits inflated from flate", "[/FlateDecode /ASCIIHexDecode]",
			syntheticRepeatedZlib([]byte("2020"), hostileNaiveExpansionBytes), false,
		},
		{
			"ascii85 zero groups inflated from flate", "[/FlateDecode /ASCII85Decode]",
			syntheticRepeatedZlib([]byte("z"), hostileNaiveExpansionBytes), false,
		},
		{
			"lzw zeros beyond the stream cap", "/LZWDecode",
			hostileLZWZerosBeyondTheStreamCap(), false,
		},
		{
			"lzw under flate", "[/FlateDecode /LZWDecode]",
			syntheticZlib(hostileLZWZerosBeyondTheStreamCap()), false,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			parallelWhenAllocationIsUnmeasured(t)
			body := newSyntheticPDF().
				page(1, "/Contents [2 0 R 3 0 R]").
				rawStream(2, "", "BT ("+hostileLegitimatePageText+") Tj ET").
				encodedStream(3, "/Filter "+testCase.filters, testCase.encoded).
				finished()
			extraction := requireBoundedExtraction(t, body)
			if !extraction.readable ||
				!strings.HasPrefix(extraction.document.Text, hostileLegitimatePageText) {
				t.Fatalf("chain extraction = %t %q", extraction.readable,
					extraction.document.Text[:min(len(extraction.document.Text), 128)])
			}
			if reachedCap := len(
				extraction.document.Text,
			) > pdfMaxTextBytes/2; reachedCap != testCase.capped {
				t.Fatalf("text bytes = %d, cap reached %t, want %t",
					len(extraction.document.Text), reachedCap, testCase.capped)
			}
		})
	}
}

func TestHostileFilterChainsDecodeLegitimateTextAndRefuseBrokenStages(t *testing.T) {
	parallelWhenAllocationIsUnmeasured(t)
	content := []byte("BT (Chained filter stage words) Tj ET")
	cases := []struct {
		name     string
		filters  string
		encoded  []byte
		readable bool
	}{
		{"flate", "/FlateDecode", syntheticZlib(content), true},
		{"lzw", "/LZWDecode", syntheticLZW(content), true},
		{"ascii hex", "/ASCIIHexDecode", syntheticASCIIHex(content), true},
		{"ascii85", "/ASCII85Decode", syntheticASCII85(content), true},
		{
			"three flate levels", "[/FlateDecode /FlateDecode /FlateDecode]",
			syntheticZlib(syntheticZlib(syntheticZlib(content))), true,
		},
		{
			"ascii85 over flate", "[/ASCII85Decode /FlateDecode]",
			syntheticASCII85(syntheticZlib(content)), true,
		},
		{"truncated flate", "/FlateDecode", syntheticZlib(content)[:4], false},
		{"flate filter over plain bytes", "/FlateDecode", content, false},
		{"invalid ascii hex digits", "/ASCIIHexDecode", []byte("zz>"), false},
		{"unsupported image filter", "/DCTDecode", content, false},
		{
			"unsupported stage inside a chain",
			"[/FlateDecode /JBIG2Decode]",
			syntheticZlib(content),
			false,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			parallelWhenAllocationIsUnmeasured(t)
			body := newSyntheticPDF().
				page(1, "/Contents 2 0 R").
				encodedStream(2, "/Filter "+testCase.filters, testCase.encoded).
				finished()
			extraction := requireBoundedExtraction(t, body)
			if extraction.readable != testCase.readable {
				t.Fatalf("readable = %t, want %t: %q", extraction.readable, testCase.readable,
					extraction.document.Text)
			}
			if testCase.readable && extraction.document.Text != "Chained filter stage words" {
				t.Fatalf("chain text = %q", extraction.document.Text)
			}
		})
	}
}

func TestHostileStreamsBeyondTheStreamCapAreNotRead(t *testing.T) {
	parallelWhenAllocationIsUnmeasured(t)
	pdf := newSyntheticPDF()
	references := make([]int, 0, pdfMaxStreams+1)
	for index := range pdfMaxStreams + 1 {
		references = append(references, 2+index)
	}
	pdf.page(1, "/Contents "+contentsArray(references...))
	for index, reference := range references {
		word := "filler"
		switch index {
		case 0:
			word = "Firstwordofthecappedpage"
		case pdfMaxStreams - 1:
			word = "Lastwordwithinthecap"
		case pdfMaxStreams:
			word = "Wordbeyondthestreamcap"
		}
		pdf.rawStream(reference, "", fmt.Sprintf("BT (%s) Tj ET", word))
	}
	extraction := requireBoundedExtraction(t, pdf.finished())
	requireTextContains(
		t,
		extraction.document.Text,
		"Firstwordofthecappedpage",
		"Lastwordwithinthecap",
	)
	requireTextOmits(t, extraction.document.Text, "Wordbeyondthestreamcap")
}
