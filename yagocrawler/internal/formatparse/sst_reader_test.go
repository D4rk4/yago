package formatparse

import (
	"strings"
	"testing"
)

// TestSSTReaderSpansContinue proves a shared string whose characters spill
// into a CONTINUE segment is reassembled, re-reading the encoding flag byte at
// the boundary — the notorious BIFF8 edge the naive readers get wrong.
func TestSSTReaderSpansContinue(t *testing.T) {
	// First segment: header for one 8-char string plus the first four
	// compressed characters; the CONTINUE segment repeats the flag then the
	// remaining characters, switching to the high-byte encoding.
	seg1 := make([]byte, 0, 8)
	seg1 = append(seg1, 0x08, 0x00, 0x00) // cch=8, grbit compressed.
	seg1 = append(seg1, []byte("Cont")...)
	seg2 := make([]byte, 0, 9)
	seg2 = append(seg2, 0x01) // continuation flag: now high-byte.
	seg2 = append(seg2, utf16LE("inue")...)

	reader := &sstReader{segments: [][]byte{seg1, seg2}}
	got := reader.readAll(1)
	if len(got) != 1 || got[0] != "Continue" {
		t.Fatalf("spanned string = %#v", got)
	}
}

// TestSSTReaderExtendedTrailer covers the phonetic (fExtSt) trailer skip and a
// short/absent table.
func TestSSTReaderExtendedTrailer(t *testing.T) {
	seg := []byte{0x03, 0x00, 0x04} // cch=3, grbit fExtSt.
	seg = le32(seg, 2)              // cbExtRst: two trailing bytes to skip.
	seg = append(seg, []byte("Abc")...)
	seg = append(seg, 0xEE, 0xEE) // phonetic bytes.
	seg = append(seg, []byte{0x02, 0x00, 0x00}...)
	seg = append(seg, []byte("Zz")...)

	reader := &sstReader{segments: [][]byte{seg}}
	got := reader.readAll(2)
	if len(got) != 2 || got[0] != "Abc" || got[1] != "Zz" {
		t.Fatalf("ext trailer strings = %#v", got)
	}

	empty := &sstReader{segments: nil}
	if got := empty.readAll(3); len(got) != 0 {
		t.Fatalf("empty table = %#v", got)
	}
}

// TestSharedStringsWithoutRecord returns nil when no SST record is present.
func TestSharedStringsWithoutRecord(t *testing.T) {
	if got := sharedStrings([]biffRecord{{recType: 0x0009}}); got != nil {
		t.Fatalf("shared strings without SST = %#v", got)
	}
}

// TestReadXLUnicodeShort guards the length checks in the label reader.
func TestReadXLUnicodeShort(t *testing.T) {
	if got := readXLUnicode([]byte{0x01}); got != "" {
		t.Fatalf("short label = %q", got)
	}
	high := make([]byte, 0, 7)
	high = append(high, 0x02, 0x00, 0x01)
	high = append(high, utf16LE("Hi")...)
	if got := readXLUnicode(high); got != "Hi" {
		t.Fatalf("high-byte label = %q", got)
	}
}

// TestBiffRecordsStopsOnTruncation ends record parsing at a short tail.
func TestBiffRecordsStopsOnTruncation(t *testing.T) {
	stream := biffRecord0(0x0009, []byte("ok"))
	stream = append(stream, 0x10, 0x00, 0xFF, 0x00) // claims 255 bytes, has none.
	records := biffRecords(stream)
	if len(records) != 1 || !strings.Contains(string(records[0].data), "ok") {
		t.Fatalf("records = %#v", records)
	}
}

// TestSSTReaderSkipSpansSegments drives the rich-run and phonetic skips across
// a CONTINUE boundary and a header read that runs past the end.
func TestSSTReaderSkipSpansSegments(t *testing.T) {
	// A rich string whose 4-byte formatting run is split across two segments.
	seg1 := []byte{0x02, 0x00, 0x08} // cch=2, grbit fRichSt.
	seg1 = le16(seg1, 2)             // cRun=2 -> 8 bytes of runs to skip.
	seg1 = append(seg1, []byte("Hi")...)
	seg1 = append(seg1, 0x01, 0x02, 0x03)        // first 3 of 8 run bytes.
	seg2 := []byte{0x04, 0x05, 0x06, 0x07, 0x08} // remaining run bytes.

	reader := &sstReader{segments: [][]byte{seg1, seg2}}
	got := reader.readAll(1)
	if len(got) != 1 || got[0] != "Hi" {
		t.Fatalf("rich skip spanning = %#v", got)
	}

	// A header read past the end returns zero values without panicking.
	empty := &sstReader{segments: [][]byte{{}}}
	if empty.readU32() != 0 || empty.readU16() != 0 {
		t.Fatal("reads past the end must be zero")
	}
}
