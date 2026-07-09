package formatparse

import (
	"encoding/binary"
	"errors"
	"unicode/utf16"
)

// Minimal reader for the OLE2 / Compound File Binary Format (MS-CFB) that the
// legacy binary Office documents use — Word .doc, Excel .xls, PowerPoint .ppt,
// Visio .vsd, and Outlook .msg are all compound files. The whole point is to
// isolate the individual named streams (WordDocument, Workbook, PowerPoint
// Document, ...) so a format-specific extractor sees only the content it wants
// instead of scanning the raw container — the latter leaks OLE directory
// names and embedded-image bytes into the index. Only stdlib is used.

const (
	// cfbHeaderSize is the fixed 512-byte header regardless of sector size.
	cfbHeaderSize = 512
	// cfbDirEntrySize is the fixed directory-entry stride.
	cfbDirEntrySize = 128
	// cfbFreeSect and friends are the reserved FAT chain terminators.
	cfbEndOfChain = 0xFFFFFFFE
	cfbFreeSect   = 0xFFFFFFFF
	// cfbMaxSectors caps chain walks so a corrupt or hostile file cannot spin.
	cfbMaxSectors = 1 << 20
	// cfbMaxStream bounds a single extracted stream.
	cfbMaxStream = officeMaxPartBytes
)

var (
	errNotCompoundFile = errors.New("not an OLE2 compound file")
	errCompoundCorrupt = errors.New("corrupt OLE2 compound file")
)

// cfbSignature marks a compound file.
var cfbSignature = []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}

// compoundFile is a parsed compound-file directory ready for stream reads.
type compoundFile struct {
	data          []byte
	sectorSize    int
	miniSectorCut uint32
	fat           []uint32
	miniFAT       []uint32
	entries       []cfbEntry
	miniStream    []byte
}

// cfbEntry is one directory record: a storage, a stream, or the root.
type cfbEntry struct {
	name      string
	kind      byte
	startSect uint32
	size      uint64
}

// openCompoundFile parses the header, FAT, mini-FAT, and directory.
func openCompoundFile(body []byte) (*compoundFile, error) {
	if len(body) < cfbHeaderSize || string(body[:8]) != string(cfbSignature) {
		return nil, errNotCompoundFile
	}
	sectorSize := 1 << binary.LittleEndian.Uint16(body[0x1E:])
	if sectorSize != 512 && sectorSize != 4096 {
		return nil, errCompoundCorrupt
	}
	file := &compoundFile{
		data:          body,
		sectorSize:    sectorSize,
		miniSectorCut: binary.LittleEndian.Uint32(body[0x38:]),
	}
	if err := file.loadFAT(); err != nil {
		return nil, err
	}
	if err := file.loadDirectory(); err != nil {
		return nil, err
	}
	file.loadMiniFAT()

	return file, nil
}

// sector returns the raw bytes of sector n, or nil when out of range.
func (c *compoundFile) sector(n uint32) []byte {
	start := (int(n) + 1) * c.sectorSize
	end := start + c.sectorSize
	if start < 0 || end > len(c.data) || start > end {
		return nil
	}

	return c.data[start:end]
}

// loadFAT assembles the FAT from the DIFAT entries in the header plus any
// DIFAT sectors chained after them.
func (c *compoundFile) loadFAT() error {
	fatSectors := c.difatSectors()
	c.fat = make([]uint32, 0, len(fatSectors)*(c.sectorSize/4))
	for _, sect := range fatSectors {
		raw := c.sector(sect)
		if raw == nil {
			return errCompoundCorrupt
		}
		for off := 0; off+4 <= len(raw); off += 4 {
			c.fat = append(c.fat, binary.LittleEndian.Uint32(raw[off:]))
		}
	}
	if len(c.fat) == 0 {
		return errCompoundCorrupt
	}

	return nil
}

// difatSectors lists the FAT-sector numbers: the first 109 come from the
// header DIFAT array; the rest are chained through DIFAT sectors.
func (c *compoundFile) difatSectors() []uint32 {
	sectors := make([]uint32, 0, 109)
	for i := 0; i < 109; i++ {
		value := binary.LittleEndian.Uint32(c.data[0x4C+i*4:])
		if value == cfbFreeSect || value == cfbEndOfChain {
			continue
		}
		sectors = append(sectors, value)
	}
	next := binary.LittleEndian.Uint32(c.data[0x44:])
	guard := 0
	for next != cfbEndOfChain && next != cfbFreeSect && guard < cfbMaxSectors {
		guard++
		raw := c.sector(next)
		if raw == nil {
			break
		}
		for off := 0; off+4 <= len(raw)-4; off += 4 {
			if value := binary.LittleEndian.Uint32(raw[off:]); value != cfbFreeSect {
				sectors = append(sectors, value)
			}
		}
		next = binary.LittleEndian.Uint32(raw[len(raw)-4:])
	}

	return sectors
}

// chain walks a FAT sector chain from start, returning the ordered sectors.
func (c *compoundFile) chain(start uint32, table []uint32) []uint32 {
	sectors := make([]uint32, 0, 8)
	current := start
	for current != cfbEndOfChain && current != cfbFreeSect {
		if len(sectors) >= cfbMaxSectors || int(current) >= len(table) {
			break
		}
		sectors = append(sectors, current)
		current = table[current]
	}

	return sectors
}

// readChain concatenates the bytes of a regular FAT chain up to size.
func (c *compoundFile) readChain(start uint32, size uint64) []byte {
	if size > cfbMaxStream {
		size = cfbMaxStream
	}
	out := make([]byte, 0, size)
	for _, sect := range c.chain(start, c.fat) {
		raw := c.sector(sect)
		if raw == nil {
			break
		}
		out = append(out, raw...)
		if uint64(len(out)) >= size {
			break
		}
	}
	if uint64(len(out)) > size {
		out = out[:size]
	}

	return out
}

// loadDirectory reads every directory entry from the directory chain.
func (c *compoundFile) loadDirectory() error {
	dirStart := binary.LittleEndian.Uint32(c.data[0x30:])
	var raw []byte
	for _, sect := range c.chain(dirStart, c.fat) {
		block := c.sector(sect)
		if block == nil {
			return errCompoundCorrupt
		}
		raw = append(raw, block...)
	}
	for off := 0; off+cfbDirEntrySize <= len(raw); off += cfbDirEntrySize {
		c.entries = append(c.entries, decodeDirEntry(raw[off:off+cfbDirEntrySize]))
	}
	if len(c.entries) == 0 {
		return errCompoundCorrupt
	}

	return nil
}

// decodeDirEntry reads one 128-byte directory record.
func decodeDirEntry(raw []byte) cfbEntry {
	nameLen := int(binary.LittleEndian.Uint16(raw[0x40:]))
	if nameLen > 64 {
		nameLen = 64
	}
	name := ""
	if nameLen >= 2 {
		units := make([]uint16, 0, nameLen/2)
		for i := 0; i+1 < nameLen; i += 2 {
			if unit := binary.LittleEndian.Uint16(raw[i:]); unit != 0 {
				units = append(units, unit)
			}
		}
		name = string(utf16.Decode(units))
	}

	return cfbEntry{
		name:      name,
		kind:      raw[0x42],
		startSect: binary.LittleEndian.Uint32(raw[0x74:]),
		size:      binary.LittleEndian.Uint64(raw[0x78:]),
	}
}

// loadMiniFAT reads the mini-FAT chain and the root entry's mini stream.
func (c *compoundFile) loadMiniFAT() {
	if root, ok := c.rootEntry(); ok {
		c.miniStream = c.readChain(root.startSect, root.size)
	}
	miniStart := binary.LittleEndian.Uint32(c.data[0x3C:])
	for _, sect := range c.chain(miniStart, c.fat) {
		raw := c.sector(sect)
		if raw == nil {
			break
		}
		for off := 0; off+4 <= len(raw); off += 4 {
			c.miniFAT = append(c.miniFAT, binary.LittleEndian.Uint32(raw[off:]))
		}
	}
}

// rootEntry returns the root storage entry (object type 5).
func (c *compoundFile) rootEntry() (cfbEntry, bool) {
	for _, entry := range c.entries {
		if entry.kind == 5 {
			return entry, true
		}
	}

	return cfbEntry{}, false
}

// stream returns the bytes of the named stream, or nil when it is absent.
func (c *compoundFile) stream(name string) []byte {
	for _, entry := range c.entries {
		if entry.kind == 2 && entry.name == name {
			return c.readStream(entry)
		}
	}

	return nil
}

// readStream reads a stream from either the regular FAT or, for small
// streams, the mini-FAT backed by the root mini stream.
func (c *compoundFile) readStream(entry cfbEntry) []byte {
	if entry.size >= uint64(c.miniSectorCut) {
		return c.readChain(entry.startSect, entry.size)
	}

	return c.readMiniChain(entry.startSect, entry.size)
}

// readMiniChain concatenates 64-byte mini sectors from the mini stream.
func (c *compoundFile) readMiniChain(start uint32, size uint64) []byte {
	if size > cfbMaxStream {
		size = cfbMaxStream
	}
	const miniSectorSize = 64
	out := make([]byte, 0, size)
	for _, sect := range c.chain(start, c.miniFAT) {
		begin := int(sect) * miniSectorSize
		end := begin + miniSectorSize
		if begin < 0 || end > len(c.miniStream) {
			break
		}
		out = append(out, c.miniStream[begin:end]...)
		if uint64(len(out)) >= size {
			break
		}
	}
	if uint64(len(out)) > size {
		out = out[:size]
	}

	return out
}
