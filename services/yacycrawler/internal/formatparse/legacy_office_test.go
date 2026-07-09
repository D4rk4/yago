package formatparse

import (
	"encoding/binary"
	"strings"
	"testing"
	"unicode/utf16"
)

// le16 / le32 append little-endian integers, keeping the binary fixtures below
// readable.
func le16(dst []byte, v uint16) []byte {
	var b [2]byte
	binary.LittleEndian.PutUint16(b[:], v)

	return append(dst, b[:]...)
}

func le32(dst []byte, v uint32) []byte {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], v)

	return append(dst, b[:]...)
}

func utf16LE(s string) []byte {
	var out []byte
	for _, u := range utf16.Encode([]rune(s)) {
		out = le16(out, u)
	}

	return out
}

// TestExtractWordDoc drives the Word piece table through both a compressed
// (cp1252) piece and an uncompressed (UTF-16) piece, exercising the control
// characters and the field-code skip.
func TestExtractWordDoc(t *testing.T) {
	const headerLen = 0x200
	piece1 := []byte("Alpha")                         // compressed cp1252.
	piece1 = append(piece1, 0x92, 'b', 'e', 't', 'a') // 0x92 -> U+2019.
	piece1 = append(piece1, 0x0D, 'G', 'a', 'm', 'a') // 0x0D -> newline.

	piece2 := make([]byte, 0, 64) // UTF-16 with a field code and control characters.
	piece2 = append(piece2, utf16LE("Delta")...)
	piece2 = append(piece2, utf16LE("\x13CODE\x14Res\x15\x07End\x09X\x1EY\x1F")...)

	wordDoc := make([]byte, 0, headerLen+len(piece1)+len(piece2))
	wordDoc = append(wordDoc, make([]byte, headerLen)...)
	binary.LittleEndian.PutUint16(wordDoc[0:], 0xA5EC)
	binary.LittleEndian.PutUint16(wordDoc[0x0A:], 0x0200) // use 1Table.
	p1 := u32(len(wordDoc))
	wordDoc = append(wordDoc, piece1...)
	p2 := u32(len(wordDoc))
	wordDoc = append(wordDoc, piece2...)

	table := buildWordTable(p1, p2, u32(len(piece1)), u32(len(piece2)/2))
	// The FIB records the CLX location in the table stream: it sits at the
	// start (fcClx=0) and spans the whole table.
	binary.LittleEndian.PutUint32(wordDoc[0x01A2:], 0)
	binary.LittleEndian.PutUint32(wordDoc[0x01A6:], u32(len(table)))
	body := buildCompoundFile([]cfbStream{
		{name: "WordDocument", data: wordDoc},
		{name: "1Table", data: table},
	})

	page, parsed := parseLegacyOffice("https://a.example/report.doc", body)
	if !parsed {
		t.Fatal("word doc did not parse")
	}
	for _, want := range []string{"Alpha", "beta", "Gama", "Delta", "Res", "End", "X-Y"} {
		if !strings.Contains(page.Text, want) {
			t.Fatalf("word text missing %q in %q", want, page.Text)
		}
	}
	if strings.Contains(page.Text, "CODE") || strings.Contains(page.Text, "Z") {
		t.Fatalf("word field code / dropped char leaked: %q", page.Text)
	}
	if !strings.Contains(page.Text, "’") {
		t.Fatalf("cp1252 punctuation not decoded: %q", page.Text)
	}
}

// buildWordTable assembles a CLX with a leading Prc and a two-piece Pcdt.
func buildWordTable(p1, p2, chars1, chars2 uint32) []byte {
	var plc []byte
	plc = le32(plc, 0)              // aCP[0].
	plc = le32(plc, chars1)         // aCP[1].
	plc = le32(plc, chars1+chars2)  // aCP[2].
	plc = appendPCD(plc, p1, true)  // compressed piece.
	plc = appendPCD(plc, p2, false) // uncompressed piece.

	var clx []byte
	clx = append(clx, 0x01)       // Prc marker.
	clx = le16(clx, 2)            // cbGrpprl.
	clx = append(clx, 0x00, 0x00) // grpprl bytes (ignored).
	clx = append(clx, 0x02)       // Pcdt marker.
	clx = le32(clx, u32(len(plc)))
	clx = append(clx, plc...)

	return clx
}

// appendPCD writes one 8-byte piece descriptor for byte offset off.
func appendPCD(dst []byte, off uint32, compressed bool) []byte {
	dst = le16(dst, 0) // PCD flags.
	if compressed {
		dst = le32(dst, (off<<1)|0x40000000)
	} else {
		dst = le32(dst, off)
	}

	return le16(dst, 0) // prm.
}

// TestExtractExcelWorkbook drives the shared-string table (compressed,
// high-byte, and rich strings) plus an inline label cell.
func TestExtractExcelWorkbook(t *testing.T) {
	var sst []byte
	sst = le32(sst, 3) // cstTotal.
	sst = le32(sst, 3) // cstUnique.
	sst = append(sst, xlString("Apple", false, false)...)
	sst = append(sst, xlString("Bär", true, false)...)
	sst = append(sst, xlString("Rich", false, true)...)

	var label []byte
	label = le16(label, 0)      // row.
	label = le16(label, 0)      // col.
	label = le16(label, 0)      // ixfe.
	label = le16(label, 6)      // cch.
	label = append(label, 0x00) // grbit compressed.
	label = append(label, []byte("Inline")...)

	workbook := biffRecord0(0x00FC, sst)
	workbook = append(workbook, biffRecord0(0x0204, label)...)
	body := buildCompoundFile([]cfbStream{{name: "Workbook", data: workbook}})

	page, parsed := parseLegacyOffice("https://a.example/sheet.xls", body)
	if !parsed {
		t.Fatal("xls did not parse")
	}
	for _, want := range []string{"Apple", "Bär", "Rich", "Inline"} {
		if !strings.Contains(page.Text, want) {
			t.Fatalf("xls text missing %q in %q", want, page.Text)
		}
	}
}

// xlString builds an XLUnicodeRichExtendedString.
func xlString(text string, highByte, rich bool) []byte {
	var out []byte
	out = le16(out, u16(len([]rune(text))))
	grbit := byte(0)
	if highByte {
		grbit |= 0x01
	}
	if rich {
		grbit |= 0x08
	}
	out = append(out, grbit)
	if rich {
		out = le16(out, 1) // cRun.
	}
	if highByte {
		out = append(out, utf16LE(text)...)
	} else {
		out = append(out, []byte(text)...)
	}
	if rich {
		out = append(out, 0, 0, 0, 0) // one 4-byte formatting run to skip.
	}

	return out
}

// biffRecord0 frames a BIFF record.
func biffRecord0(recType uint16, data []byte) []byte {
	out := le16(nil, recType)
	out = le16(out, u16(len(data)))

	return append(out, data...)
}

// TestExtractPowerPoint drives the two PowerPoint text atoms nested inside a
// container, with a non-text atom to cover the ignored branch.
func TestExtractPowerPoint(t *testing.T) {
	children := pptAtom(0x0FA8, []byte("Slide bytes text")) // TextBytesAtom.
	children = append(children, pptAtom(0x0FA0, utf16LE("Slide chars text"))...)
	children = append(children, pptAtom(0x0001, []byte("junk"))...)
	stream := pptContainer(children)
	body := buildCompoundFile([]cfbStream{{name: "PowerPoint Document", data: stream}})

	page, parsed := parseLegacyOffice("https://a.example/deck.ppt", body)
	if !parsed {
		t.Fatal("ppt did not parse")
	}
	for _, want := range []string{"Slide bytes text", "Slide chars text"} {
		if !strings.Contains(page.Text, want) {
			t.Fatalf("ppt text missing %q in %q", want, page.Text)
		}
	}
}

// TestPowerPointDepthGuard proves deep nesting stops instead of recursing away.
func TestPowerPointDepthGuard(t *testing.T) {
	stream := pptAtom(0x0FA8, []byte("Deep text"))
	for i := 0; i < 40; i++ {
		stream = pptContainer(stream)
	}
	body := buildCompoundFile([]cfbStream{{name: "PowerPoint Document", data: stream}})
	page, parsed := parseLegacyOffice("https://a.example/deep.ppt", body)
	if parsed && strings.Contains(page.Text, "Deep text") {
		t.Fatal("depth guard should have stopped before the buried atom")
	}
}

// pptAtom frames a PowerPoint atom record.
func pptAtom(recType uint16, data []byte) []byte {
	out := le16(nil, 0x0000) // recVer/recInstance: not a container.
	out = le16(out, recType)
	out = le32(out, u32(len(data)))

	return append(out, data...)
}

// pptContainer frames a PowerPoint container record around its children.
func pptContainer(children []byte) []byte {
	out := le16(nil, 0x000F) // recVer 0xF marks a container.
	out = le16(out, 0x0FF0)
	out = le32(out, u32(len(children)))

	return append(out, children...)
}
