package formatparse

import (
	"encoding/binary"
	"strings"
)

// sstReader decodes the BIFF8 shared-string table, whose strings can spill
// across CONTINUE records — at each such boundary the encoding flag byte is
// repeated for the remaining characters. The reader walks the SST payload and
// its CONTINUE segments as one logical byte stream, re-reading that flag when a
// string's characters cross a segment edge.
type sstReader struct {
	segments [][]byte
	seg      int
	offset   int
}

// sstMaxChars bounds a single string so a corrupt length cannot balloon.
const sstMaxChars = 1 << 20

// readAll decodes up to count strings from the table.
func (r *sstReader) readAll(count int) []string {
	strs := make([]string, 0, count)
	for k := 0; k < count && !r.atEnd(); k++ {
		strs = append(strs, r.readString())
	}

	return strs
}

// readString decodes one XLUnicodeRichExtendedString: header, characters,
// then the rich-run and phonetic trailers that follow the text.
func (r *sstReader) readString() string {
	cch := int(r.readU16())
	grbit := r.readByte()
	high := grbit&0x01 != 0
	var cRun, cbExt int
	if grbit&0x08 != 0 {
		cRun = int(r.readU16())
	}
	if grbit&0x04 != 0 {
		cbExt = int(r.readU32())
	}
	text := r.readChars(cch, high)
	r.skip(cRun * 4)
	r.skip(cbExt)

	return text
}

// readChars reads cch characters, re-reading the flag byte whenever the run
// crosses into the next CONTINUE segment.
func (r *sstReader) readChars(cch int, high bool) string {
	if cch < 0 || cch > sstMaxChars {
		return ""
	}
	var out strings.Builder
	for i := 0; i < cch; i++ {
		if r.offset >= r.segLen() {
			if !r.nextSegment() {
				break
			}
			high = r.readByte()&0x01 != 0
		}
		if high {
			out.WriteRune(rune(r.readU16()))
		} else {
			out.WriteRune(decodeCP1252(r.readByte()))
		}
	}

	return out.String()
}

// segLen is the length of the current segment, or 0 past the end.
func (r *sstReader) segLen() int {
	if r.seg >= len(r.segments) {
		return 0
	}

	return len(r.segments[r.seg])
}

// atEnd reports whether every segment is consumed.
func (r *sstReader) atEnd() bool {
	return r.seg >= len(r.segments) || (r.seg == len(r.segments)-1 && r.offset >= r.segLen())
}

// nextSegment advances to the next CONTINUE segment.
func (r *sstReader) nextSegment() bool {
	r.seg++
	r.offset = 0

	return r.seg < len(r.segments)
}

// readByte reads one byte, advancing across segment boundaries.
func (r *sstReader) readByte() byte {
	for r.seg < len(r.segments) && r.offset >= r.segLen() {
		if !r.nextSegment() {
			return 0
		}
	}
	if r.seg >= len(r.segments) {
		return 0
	}
	b := r.segments[r.seg][r.offset]
	r.offset++

	return b
}

// readU16 reads a little-endian uint16.
func (r *sstReader) readU16() uint16 {
	lo := uint16(r.readByte())
	hi := uint16(r.readByte())

	return lo | hi<<8
}

// readU32 reads a little-endian uint32.
func (r *sstReader) readU32() uint32 {
	var buf [4]byte
	for i := range buf {
		buf[i] = r.readByte()
	}

	return binary.LittleEndian.Uint32(buf[:])
}

// skip discards n bytes, crossing segment boundaries without re-reading flags.
func (r *sstReader) skip(n int) {
	for n > 0 {
		if r.offset >= r.segLen() {
			if !r.nextSegment() {
				return
			}

			continue
		}
		step := r.segLen() - r.offset
		if step > n {
			step = n
		}
		r.offset += step
		n -= step
	}
}
