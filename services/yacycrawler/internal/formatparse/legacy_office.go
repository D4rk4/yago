package formatparse

import (
	"encoding/binary"
	"strings"
	"unicode/utf16"
)

// Legacy binary Office extraction (MS-DOC / MS-XLS / MS-PPT / Visio). These
// are OLE2 compound files; scanning the whole container leaks stream names and
// embedded-image bytes, so each format is decoded from its own text stream:
// Word through the piece table, Excel through the shared-string table and
// label cells, PowerPoint through its text atoms. Visio and any other compound
// file fall back to readable runs over content streams only. A file that
// yields nothing indexable stays unparsed.

// parseLegacyOffice routes a compound-file document to its extractor by
// extension, falling back to content-stream runs for the rest.
func parseLegacyOffice(rawURL string, body []byte) (Document, bool) {
	file, err := openCompoundFile(body)
	if err != nil {
		return Document{URL: rawURL}, false
	}
	var text string
	switch urlExtension(rawURL) {
	case "doc":
		text = extractWordDoc(file)
	case "xls", "xla":
		text = extractExcelWorkbook(file)
	case "ppt", "pps":
		text = extractPowerPoint(file)
	default:
		text = extractCompoundRuns(file)
	}
	text = strings.TrimSpace(collapseBlankRuns(text))
	if text == "" {
		return Document{URL: rawURL}, false
	}

	return Document{URL: rawURL, Title: textTitle(text), Text: text}, true
}

// extractWordDoc decodes the main document text of a Word 97-2003 file through
// its piece table, the format Word uses once a document is fast-saved.
func extractWordDoc(file *compoundFile) string {
	wordDoc := file.stream("WordDocument")
	if len(wordDoc) < 0x0200 || binary.LittleEndian.Uint16(wordDoc) != 0xA5EC {
		return ""
	}
	tableName := "0Table"
	if binary.LittleEndian.Uint16(wordDoc[0x0A:])&0x0200 != 0 {
		tableName = "1Table"
	}
	table := file.stream(tableName)
	if table == nil {
		table = wordDoc
	}
	fcClx := binary.LittleEndian.Uint32(wordDoc[0x01A2:])
	lcbClx := binary.LittleEndian.Uint32(wordDoc[0x01A6:])
	pieces := wordPieceTable(table, fcClx, lcbClx)

	var out strings.Builder
	for _, piece := range pieces {
		appendWordPiece(&out, wordDoc, piece)
	}

	return out.String()
}

// wordPiece is one run of characters located in the WordDocument stream.
type wordPiece struct {
	fcByte     uint32
	charCount  uint32
	compressed bool
}

// wordPieceTable finds the Pcdt inside the CLX and decodes its piece array.
func wordPieceTable(table []byte, fcClx, lcbClx uint32) []wordPiece {
	end := uint64(fcClx) + uint64(lcbClx)
	if lcbClx == 0 || end > uint64(len(table)) {
		return nil
	}
	clx := table[fcClx:end]
	pos := skipClxPrc(clx)
	if pos+5 > len(clx) || clx[pos] != 0x02 {
		return nil
	}
	lcbPlc := binary.LittleEndian.Uint32(clx[pos+1:])
	plcStart := pos + 5
	if lcbPlc < 4 || plcStart+int(lcbPlc) > len(clx) {
		return nil
	}
	plc := clx[plcStart : plcStart+int(lcbPlc)]

	return decodePieces(plc)
}

// skipClxPrc advances past the leading Prc entries (clxt 0x01) that may
// precede the Pcdt (clxt 0x02) inside a CLX.
func skipClxPrc(clx []byte) int {
	pos := 0
	for pos+3 <= len(clx) && clx[pos] == 0x01 {
		cb := int(binary.LittleEndian.Uint16(clx[pos+1:]))
		pos += 3 + cb
	}

	return pos
}

// decodePieces splits a PlcPcd into its character-position and descriptor
// arrays and yields one wordPiece per descriptor.
func decodePieces(plc []byte) []wordPiece {
	count := (len(plc) - 4) / 12
	if count <= 0 {
		return nil
	}
	cpAt := func(i int) uint32 { return binary.LittleEndian.Uint32(plc[i*4:]) }
	pcdBase := (count + 1) * 4
	pieces := make([]wordPiece, 0, count)
	for i := 0; i < count; i++ {
		pcd := plc[pcdBase+i*8:]
		fc := binary.LittleEndian.Uint32(pcd[2:])
		piece := wordPiece{charCount: cpAt(i+1) - cpAt(i)}
		if fc&0x40000000 != 0 {
			piece.compressed = true
			piece.fcByte = (fc & 0x3FFFFFFF) / 2
		} else {
			piece.fcByte = fc & 0x3FFFFFFF
		}
		pieces = append(pieces, piece)
	}

	return pieces
}

// appendWordPiece decodes one piece's characters and appends cleaned text.
func appendWordPiece(out *strings.Builder, wordDoc []byte, piece wordPiece) {
	if piece.charCount == 0 || piece.charCount > cfbMaxStream {
		return
	}
	field := false
	for i := uint32(0); i < piece.charCount; i++ {
		r, ok := wordCharAt(wordDoc, piece, i)
		if !ok {
			return
		}
		writeWordRune(out, r, &field)
	}
}

// wordCharAt reads the i-th character of a piece from the WordDocument stream.
func wordCharAt(wordDoc []byte, piece wordPiece, i uint32) (rune, bool) {
	if piece.compressed {
		off := int(piece.fcByte + i)
		if off >= len(wordDoc) {
			return 0, false
		}

		return decodeCP1252(wordDoc[off]), true
	}
	off := int(piece.fcByte + i*2)
	if off+1 >= len(wordDoc) {
		return 0, false
	}

	return rune(binary.LittleEndian.Uint16(wordDoc[off:])), true
}

// writeWordRune maps Word's control characters to whitespace and drops field
// instruction codes, keeping the visible field result.
func writeWordRune(out *strings.Builder, r rune, field *bool) {
	switch r {
	case 0x13:
		*field = true
	case 0x14:
		*field = false
	case 0x15:
		*field = false
	case 0x07, 0x0B, 0x0C, 0x0D:
		out.WriteByte('\n')
	case 0x1E:
		out.WriteByte('-')
	case 0xA0, 0x09:
		out.WriteByte(' ')
	default:
		if !*field && (r >= 0x20 || r == '\t') && r != 0x1F {
			out.WriteRune(r)
		}
	}
}

// extractExcelWorkbook pulls the shared-string table and inline label cells
// from a BIFF Workbook stream.
func extractExcelWorkbook(file *compoundFile) string {
	workbook := file.stream("Workbook")
	if workbook == nil {
		workbook = file.stream("Book")
	}
	if workbook == nil {
		return ""
	}
	records := biffRecords(workbook)
	var out strings.Builder
	for _, s := range sharedStrings(records) {
		out.WriteString(s)
		out.WriteByte('\n')
	}
	for _, rec := range records {
		if rec.recType == 0x0204 && len(rec.data) > 6 { // LABEL: row,col,ixfe,string.
			out.WriteString(readXLUnicode(rec.data[6:]))
			out.WriteByte('\n')
		}
	}

	return out.String()
}

// biffRecord is one BIFF type/length/data triple.
type biffRecord struct {
	recType uint16
	data    []byte
}

// biffRecords splits a BIFF stream into its records.
func biffRecords(stream []byte) []biffRecord {
	records := make([]biffRecord, 0, 64)
	for pos := 0; pos+4 <= len(stream); {
		recType := binary.LittleEndian.Uint16(stream[pos:])
		length := int(binary.LittleEndian.Uint16(stream[pos+2:]))
		pos += 4
		if pos+length > len(stream) {
			break
		}
		records = append(records, biffRecord{recType: recType, data: stream[pos : pos+length]})
		pos += length
	}

	return records
}

// sharedStrings decodes the SST record and its trailing CONTINUE payloads.
func sharedStrings(records []biffRecord) []string {
	for i, rec := range records {
		if rec.recType != 0x00FC || len(rec.data) < 8 {
			continue
		}
		segments := [][]byte{rec.data[8:]}
		for j := i + 1; j < len(records) && records[j].recType == 0x003C; j++ {
			segments = append(segments, records[j].data)
		}
		count := int(binary.LittleEndian.Uint32(rec.data[4:]))

		return (&sstReader{segments: segments}).readAll(count)
	}

	return nil
}

// readXLUnicode reads a length-prefixed BIFF8 string (cch uint16, grbit byte).
func readXLUnicode(data []byte) string {
	if len(data) < 3 {
		return ""
	}
	cch := int(binary.LittleEndian.Uint16(data))
	highByte := data[2]&0x01 != 0
	body := data[3:]
	if highByte {
		return decodeUTF16Bytes(body, cch)
	}

	return decodeCompressed(body, cch)
}

// decodeUTF16Bytes decodes cch UTF-16LE code units from body.
func decodeUTF16Bytes(body []byte, cch int) string {
	if limit := len(body) / 2; cch > limit {
		cch = limit
	}
	if cch < 0 {
		return ""
	}
	units := make([]uint16, 0, cch)
	for i := 0; i < cch; i++ {
		units = append(units, uint16(body[i*2])|uint16(body[i*2+1])<<8)
	}

	return string(utf16.Decode(units))
}

// decodeCompressed decodes cch 8-bit (cp1252) characters from body.
func decodeCompressed(body []byte, cch int) string {
	var out strings.Builder
	for i := 0; i < cch && i < len(body); i++ {
		out.WriteRune(decodeCP1252(body[i]))
	}

	return out.String()
}

// extractPowerPoint walks the PowerPoint Document stream for its text atoms.
func extractPowerPoint(file *compoundFile) string {
	stream := file.stream("PowerPoint Document")
	if stream == nil {
		return ""
	}
	var out strings.Builder
	walkPPTRecords(stream, &out, 0)

	// PowerPoint separates paragraphs with carriage returns; normalise them
	// so the shared blank-run collapse treats them as line breaks.
	return strings.ReplaceAll(out.String(), "\r", "\n")
}

// walkPPTRecords descends the PowerPoint record tree, decoding text atoms.
func walkPPTRecords(data []byte, out *strings.Builder, depth int) {
	if depth > 32 {
		return
	}
	for pos := 0; pos+8 <= len(data); {
		verInstance := binary.LittleEndian.Uint16(data[pos:])
		recType := binary.LittleEndian.Uint16(data[pos+2:])
		length := int(binary.LittleEndian.Uint32(data[pos+4:]))
		pos += 8
		if length < 0 || pos+length > len(data) {
			break
		}
		body := data[pos : pos+length]
		if verInstance&0x000F == 0x000F {
			walkPPTRecords(body, out, depth+1)
		} else {
			appendPPTText(out, recType, body)
		}
		pos += length
	}
}

// appendPPTText decodes the two PowerPoint text atoms.
func appendPPTText(out *strings.Builder, recType uint16, body []byte) {
	switch recType {
	case 0x0FA0: // TextCharsAtom: UTF-16LE.
		out.WriteString(decodeUTF16Bytes(body, len(body)/2))
		out.WriteByte('\n')
	case 0x0FA8: // TextBytesAtom: 8-bit.
		out.WriteString(decodeCompressed(body, len(body)))
		out.WriteByte('\n')
	}
}

// extractCompoundRuns is the Visio / unknown-compound fallback: readable runs
// from content streams only, skipping the metadata and OLE machinery streams.
func extractCompoundRuns(file *compoundFile) string {
	var out strings.Builder
	for _, entry := range file.entries {
		if entry.kind != 2 || metadataStream(entry.name) {
			continue
		}
		for _, run := range printableRuns(file.readStream(entry)) {
			out.WriteString(run)
			out.WriteByte('\n')
		}
	}

	return out.String()
}

// metadataStream reports whether a stream carries container machinery rather
// than document content (its runs would be noise in the index).
func metadataStream(name string) bool {
	if name == "" {
		return true
	}
	if name[0] < 0x20 { // \x01Ole, \x05SummaryInformation, ...
		return true
	}

	return name == "CompObj" || name == "ObjInfo"
}
