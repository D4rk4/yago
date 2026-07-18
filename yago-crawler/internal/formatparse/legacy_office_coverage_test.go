package formatparse

import (
	"encoding/binary"
	"strings"
	"testing"
)

// TestAppendWordPieceGuards covers the empty and over-long piece exits and the
// out-of-range character reads in both the compressed and UTF-16 branches.
func TestAppendWordPieceGuards(t *testing.T) {
	var out strings.Builder
	appendWordPiece(&out, []byte("abc"), wordPiece{charCount: 0})
	appendWordPiece(&out, []byte("abc"), wordPiece{charCount: cfbMaxStream + 1})
	if out.Len() != 0 {
		t.Fatalf("guarded pieces wrote %q", out.String())
	}

	// A compressed piece pointing past the stream end stops immediately.
	appendWordPiece(&out, []byte("abc"), wordPiece{compressed: true, fcByte: 100, charCount: 3})
	// A UTF-16 piece pointing past the stream end stops immediately.
	appendWordPiece(&out, []byte("abc"), wordPiece{fcByte: 100, charCount: 3})
	if out.Len() != 0 {
		t.Fatalf("out-of-range pieces wrote %q", out.String())
	}
}

// TestWordCharAtOutOfRange covers both bounds checks directly.
func TestWordCharAtOutOfRange(t *testing.T) {
	if _, ok := wordCharAt([]byte("ab"), wordPiece{compressed: true, fcByte: 5}, 0); ok {
		t.Fatal("compressed read past end must fail")
	}
	if _, ok := wordCharAt([]byte("ab"), wordPiece{fcByte: 5}, 0); ok {
		t.Fatal("utf-16 read past end must fail")
	}
}

// TestWordPieceTableGuards covers the malformed-CLX exits.
func TestWordPieceTableGuards(t *testing.T) {
	if got := wordPieceTable([]byte("x"), 0, 0); got != nil {
		t.Fatalf("zero lcb = %v", got)
	}
	if got := wordPieceTable([]byte("x"), 0, 99); got != nil {
		t.Fatalf("out-of-range clx = %v", got)
	}
	// A CLX with no Pcdt marker.
	noPcdt := []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	if got := wordPieceTable(noPcdt, 0, u32(len(noPcdt))); got != nil {
		t.Fatalf("missing Pcdt = %v", got)
	}
	// A Pcdt whose declared PlcPcd length overruns the CLX.
	badLen := append([]byte{0x02}, le32(nil, 0xFFFF)...)
	if got := wordPieceTable(badLen, 0, u32(len(badLen))); got != nil {
		t.Fatalf("overrun PlcPcd = %v", got)
	}
	// A Pcdt with a PlcPcd too short to hold even one piece descriptor.
	tinyPlc := append([]byte{0x02}, le32(nil, 4)...)
	tinyPlc = append(tinyPlc, 0, 0, 0, 0)
	if got := wordPieceTable(tinyPlc, 0, u32(len(tinyPlc))); got != nil {
		t.Fatalf("undersized PlcPcd = %v", got)
	}
}

// TestWordDocUsesZeroTable covers the default (0Table) selection with the
// table stream actually present.
func TestWordDocUsesZeroTable(t *testing.T) {
	const headerLen = 0x200
	text := []byte("Zero table body")
	wordDoc := make([]byte, 0, headerLen+len(text))
	wordDoc = append(wordDoc, make([]byte, headerLen)...)
	binary.LittleEndian.PutUint16(wordDoc[0:], 0xA5EC) // flags at 0x0A stay 0 -> 0Table.
	offset := u32(len(wordDoc))
	wordDoc = append(wordDoc, text...)
	table := buildWordTable(offset, offset, u32(len(text)), 0)
	binary.LittleEndian.PutUint32(wordDoc[0x01A2:], 0)
	binary.LittleEndian.PutUint32(wordDoc[0x01A6:], u32(len(table)))

	body := buildCompoundFile([]cfbStream{
		{name: "WordDocument", data: wordDoc},
		{name: "0Table", data: table},
	})
	page, parsed := parseLegacyOffice("https://a.example/zero.doc", body)
	if !parsed || !strings.Contains(page.Text, "Zero table body") {
		t.Fatalf("0Table doc = %v %+v", parsed, page)
	}
}

// TestDecodeUTF16BytesGuards covers the length clamp and the negative guard.
func TestDecodeUTF16BytesGuards(t *testing.T) {
	if got := decodeUTF16Bytes([]byte{0x41, 0x00}, 1000); got != "A" {
		t.Fatalf("clamped decode = %q", got)
	}
	if got := decodeUTF16Bytes([]byte{0x41, 0x00}, -1); got != "" {
		t.Fatalf("negative count = %q", got)
	}
}

// TestWalkPPTRecordsTruncated covers the truncated-record break.
func TestWalkPPTRecordsTruncated(t *testing.T) {
	// A header claiming more body than remains.
	stream := le16(nil, 0x0000)
	stream = le16(stream, 0x0FA8)
	stream = le32(stream, 0xFFFF) // length far beyond the buffer.
	var out strings.Builder
	walkPPTRecords(stream, &out, 0)
	if out.Len() != 0 {
		t.Fatalf("truncated ppt record wrote %q", out.String())
	}
}
