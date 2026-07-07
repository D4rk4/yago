package formatparse

import (
	"encoding/binary"
	"unicode/utf16"
)

// Test-only writer for the OLE2 compound file format. It assembles a valid
// container from named streams — routing small streams through the mini stream
// and large streams through the regular FAT — so the reader and the legacy
// extractors are exercised against real structure without checking binary
// blobs into the tree. The reader scans directory entries flatly, so the
// red-black sibling tree is left empty (NOSTREAM links).

const (
	cfbTestSectorSize = 512
	cfbTestMiniCutoff = 4096
	cfbTestMiniSector = 64
	cfbNoStream       = 0xFFFFFFFF
	cfbFATSect        = 0xFFFFFFFD
	cfbEntriesPerFAT  = cfbTestSectorSize / 4
	cfbEntriesPerDir  = cfbTestSectorSize / cfbDirEntrySize
)

// cfbStream is one named stream to place in the container.
type cfbStream struct {
	name string
	data []byte
}

// u16 / u32 narrow a fixture length; the fixtures are small and bounded, so the
// conversion cannot overflow.
func u16(n int) uint16 { return uint16(n) } //nolint:gosec // G115: bounded fixture length.
func u32(n int) uint32 { return uint32(n) } //nolint:gosec // G115: bounded fixture length.

// cfbLayout tracks the sector assignment while a container is built.
type cfbLayout struct {
	streams        []cfbStream
	miniData       []byte
	miniFAT        []uint32
	smallStart     map[int]uint32
	largeStart     map[int]uint32
	dirFirst       uint32
	miniFATFirst   uint32
	miniStreamHead uint32
	dirSectors     uint32
	miniFATSectors uint32
	miniStrSectors uint32
	total          uint32
}

// buildCompoundFile serialises the streams into a compound file.
func buildCompoundFile(streams []cfbStream) []byte {
	layout := &cfbLayout{
		streams:    streams,
		smallStart: map[int]uint32{},
		largeStart: map[int]uint32{},
	}
	layout.placeMiniStreams()
	layout.assignSectors()
	fat := layout.buildFAT()

	file := make([]byte, int(1+layout.total)*cfbTestSectorSize)
	copy(file, layout.header())
	layout.writeFAT(file, fat)
	layout.writeDirectory(file)
	layout.writeMiniFAT(file)
	layout.writeMiniStream(file)
	layout.writeLargeStreams(file)

	return file
}

// placeMiniStreams lays small streams into the mini stream and its mini-FAT.
func (l *cfbLayout) placeMiniStreams() {
	for i, s := range l.streams {
		if len(s.data) == 0 || len(s.data) >= cfbTestMiniCutoff {
			continue
		}
		start := u32(len(l.miniData) / cfbTestMiniSector)
		l.smallStart[i] = start
		sectors := (len(s.data) + cfbTestMiniSector - 1) / cfbTestMiniSector
		for k := 0; k < sectors; k++ {
			if k < sectors-1 {
				l.miniFAT = append(l.miniFAT, start+uint32(k)+1)
			} else {
				l.miniFAT = append(l.miniFAT, cfbEndOfChain)
			}
		}
		l.miniData = append(l.miniData, s.data...)
		l.miniData = append(l.miniData, make([]byte, sectors*cfbTestMiniSector-len(s.data))...)
	}
}

// assignSectors positions the FAT, directory, mini-FAT, mini stream, and each
// large stream, recording the total sector count.
func (l *cfbLayout) assignSectors() {
	entries := 1 + len(l.streams)
	l.dirSectors = ceilDiv(u32(entries), cfbEntriesPerDir)
	l.miniFATSectors = ceilDiv(u32(len(l.miniFAT)), cfbEntriesPerFAT)
	l.miniStrSectors = ceilDiv(u32(len(l.miniData)), cfbTestSectorSize)

	sec := uint32(1) // sector 0 is the single FAT sector.
	l.dirFirst, sec = sec, sec+l.dirSectors
	l.miniFATFirst, sec = sec, sec+l.miniFATSectors
	l.miniStreamHead, sec = sec, sec+l.miniStrSectors
	for i, s := range l.streams {
		if len(s.data) < cfbTestMiniCutoff {
			continue
		}
		l.largeStart[i] = sec
		sec += ceilDiv(u32(len(s.data)), cfbTestSectorSize)
	}
	l.total = sec
	if l.total > cfbEntriesPerFAT {
		panic("compound builder: fixture too large for a single FAT sector")
	}
}

// buildFAT chains every sector group in the file allocation table.
func (l *cfbLayout) buildFAT() []uint32 {
	fat := make([]uint32, l.total)
	for i := range fat {
		fat[i] = cfbFreeSect
	}
	fat[0] = cfbFATSect
	chainRange(fat, l.dirFirst, l.dirSectors)
	chainRange(fat, l.miniFATFirst, l.miniFATSectors)
	chainRange(fat, l.miniStreamHead, l.miniStrSectors)
	for i, s := range l.streams {
		if len(s.data) < cfbTestMiniCutoff {
			continue
		}
		chainRange(fat, l.largeStart[i], ceilDiv(u32(len(s.data)), cfbTestSectorSize))
	}

	return fat
}

// header builds the 512-byte compound-file header.
func (l *cfbLayout) header() []byte {
	h := make([]byte, cfbTestSectorSize)
	copy(h[:8], cfbSignature)
	binary.LittleEndian.PutUint16(h[0x18:], 0x003E)
	binary.LittleEndian.PutUint16(h[0x1A:], 0x0003)
	binary.LittleEndian.PutUint16(h[0x1C:], 0xFFFE)
	binary.LittleEndian.PutUint16(h[0x1E:], 9)
	binary.LittleEndian.PutUint16(h[0x20:], 6)
	binary.LittleEndian.PutUint32(h[0x2C:], 1)
	binary.LittleEndian.PutUint32(h[0x30:], l.dirFirst)
	binary.LittleEndian.PutUint32(h[0x38:], cfbTestMiniCutoff)
	binary.LittleEndian.PutUint32(h[0x3C:], l.miniFATHead())
	binary.LittleEndian.PutUint32(h[0x40:], l.miniFATSectors)
	binary.LittleEndian.PutUint32(h[0x44:], cfbEndOfChain)
	for i := 0; i < 109; i++ {
		binary.LittleEndian.PutUint32(h[0x4C+i*4:], cfbFreeSect)
	}
	binary.LittleEndian.PutUint32(h[0x4C:], 0)

	return h
}

// miniFATHead is the first mini-FAT sector, or ENDOFCHAIN when there is none.
func (l *cfbLayout) miniFATHead() uint32 {
	if l.miniFATSectors == 0 {
		return cfbEndOfChain
	}

	return l.miniFATFirst
}

// writeFAT writes the single FAT sector.
func (l *cfbLayout) writeFAT(file []byte, fat []uint32) {
	sector := make([]byte, cfbTestSectorSize)
	for i := 0; i < cfbEntriesPerFAT; i++ {
		value := uint32(cfbFreeSect)
		if i < len(fat) {
			value = fat[i]
		}
		binary.LittleEndian.PutUint32(sector[i*4:], value)
	}
	l.writeSector(file, 0, sector)
}

// writeDirectory writes the root entry followed by one entry per stream.
func (l *cfbLayout) writeDirectory(file []byte) {
	buf := make([]byte, 0, (1+len(l.streams))*cfbDirEntrySize)
	buf = append(buf, dirEntry("Root Entry", 5, l.rootStart(), uint64(len(l.miniData)))...)
	for i, s := range l.streams {
		buf = append(buf, dirEntry(s.name, 2, l.streamStart(i), uint64(len(s.data)))...)
	}
	l.writeGroup(file, l.dirFirst, buf)
}

// writeMiniFAT serialises the mini-FAT entries.
func (l *cfbLayout) writeMiniFAT(file []byte) {
	if l.miniFATSectors == 0 {
		return
	}
	buf := make([]byte, l.miniFATSectors*cfbTestSectorSize)
	for i := range buf {
		buf[i] = 0xFF
	}
	for i, v := range l.miniFAT {
		binary.LittleEndian.PutUint32(buf[i*4:], v)
	}
	l.writeGroup(file, l.miniFATFirst, buf)
}

// writeMiniStream writes the mini stream container.
func (l *cfbLayout) writeMiniStream(file []byte) {
	if l.miniStrSectors == 0 {
		return
	}
	l.writeGroup(file, l.miniStreamHead, l.miniData)
}

// writeLargeStreams writes each large stream's sectors.
func (l *cfbLayout) writeLargeStreams(file []byte) {
	for i, s := range l.streams {
		if len(s.data) < cfbTestMiniCutoff {
			continue
		}
		l.writeGroup(file, l.largeStart[i], s.data)
	}
}

// rootStart is the first mini-stream sector, or ENDOFCHAIN when empty.
func (l *cfbLayout) rootStart() uint32 {
	if l.miniStrSectors == 0 {
		return cfbEndOfChain
	}

	return l.miniStreamHead
}

// streamStart is the starting sector recorded for stream i.
func (l *cfbLayout) streamStart(i int) uint32 {
	if start, ok := l.smallStart[i]; ok {
		return start
	}
	if start, ok := l.largeStart[i]; ok {
		return start
	}

	return cfbEndOfChain
}

// writeGroup writes buf across consecutive sectors starting at first.
func (l *cfbLayout) writeGroup(file []byte, first uint32, buf []byte) {
	for off := 0; off < len(buf); off += cfbTestSectorSize {
		end := off + cfbTestSectorSize
		if end > len(buf) {
			end = len(buf)
		}
		sector := make([]byte, cfbTestSectorSize)
		copy(sector, buf[off:end])
		l.writeSector(file, first+uint32(off/cfbTestSectorSize), sector)
	}
}

// writeSector copies a 512-byte sector into its file position.
func (l *cfbLayout) writeSector(file []byte, n uint32, sector []byte) {
	start := int(n+1) * cfbTestSectorSize
	copy(file[start:start+cfbTestSectorSize], sector)
}

// dirEntry builds one 128-byte directory record.
func dirEntry(name string, kind byte, start uint32, size uint64) []byte {
	entry := make([]byte, cfbDirEntrySize)
	units := utf16.Encode([]rune(name))
	for i, u := range units {
		if i < 31 {
			binary.LittleEndian.PutUint16(entry[i*2:], u)
		}
	}
	nameLen := (len(units) + 1) * 2
	if nameLen > 64 {
		nameLen = 64
	}
	binary.LittleEndian.PutUint16(entry[0x40:], u16(nameLen))
	entry[0x42] = kind
	entry[0x43] = 1
	binary.LittleEndian.PutUint32(entry[0x44:], cfbNoStream)
	binary.LittleEndian.PutUint32(entry[0x48:], cfbNoStream)
	binary.LittleEndian.PutUint32(entry[0x4C:], cfbNoStream)
	binary.LittleEndian.PutUint32(entry[0x74:], start)
	binary.LittleEndian.PutUint64(entry[0x78:], size)

	return entry
}

// chainRange links n consecutive sectors from first as one FAT chain.
func chainRange(fat []uint32, first, n uint32) {
	for k := uint32(0); k < n; k++ {
		if k < n-1 {
			fat[first+k] = first + k + 1
		} else {
			fat[first+k] = cfbEndOfChain
		}
	}
}

// ceilDiv is integer ceiling division.
func ceilDiv(a, b uint32) uint32 {
	if b == 0 {
		return 0
	}

	return (a + b - 1) / b
}
