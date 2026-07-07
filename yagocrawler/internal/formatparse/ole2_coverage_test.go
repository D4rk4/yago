package formatparse

import (
	"encoding/binary"
	"errors"
	"testing"
)

// craftHeader returns a bare but signed 512-byte compound-file header with the
// DIFAT array empty, for building corruption fixtures by hand.
func craftHeader() []byte {
	h := make([]byte, cfbHeaderSize)
	copy(h[:8], cfbSignature)
	binary.LittleEndian.PutUint16(h[0x1E:], 9) // 512-byte sectors.
	binary.LittleEndian.PutUint32(h[0x38:], cfbTestMiniCutoff)
	binary.LittleEndian.PutUint32(h[0x44:], cfbEndOfChain) // no chained DIFAT.
	for i := 0; i < 109; i++ {
		binary.LittleEndian.PutUint32(h[0x4C+i*4:], cfbFreeSect)
	}

	return h
}

// TestOpenRejectsEmptyFAT covers the no-FAT-sector corruption exit.
func TestOpenRejectsEmptyFAT(t *testing.T) {
	body := append(craftHeader(), make([]byte, 512)...)
	if _, err := openCompoundFile(body); !errors.Is(err, errCompoundCorrupt) {
		t.Fatalf("empty FAT err = %v", err)
	}
}

// TestDifatChainedSectors covers the DIFAT-sector chain walk that a file with
// more than 109 FAT sectors uses.
func TestDifatChainedSectors(t *testing.T) {
	data := make([]byte, cfbHeaderSize+512)
	copy(data[:8], cfbSignature)
	binary.LittleEndian.PutUint16(data[0x1E:], 9)
	binary.LittleEndian.PutUint32(data[0x44:], 0) // first DIFAT sector is sector 0.
	for i := 0; i < 109; i++ {
		binary.LittleEndian.PutUint32(data[0x4C+i*4:], cfbFreeSect)
	}
	sector := data[cfbHeaderSize:]
	for off := 0; off+4 <= len(sector); off += 4 {
		binary.LittleEndian.PutUint32(sector[off:], cfbFreeSect)
	}
	binary.LittleEndian.PutUint32(sector[0:], 7) // a FAT sector number.
	// Chain to an out-of-range DIFAT sector so the next hop breaks.
	binary.LittleEndian.PutUint32(sector[len(sector)-4:], 1)

	file := &compoundFile{data: data, sectorSize: 512}
	if got := file.difatSectors(); len(got) != 1 || got[0] != 7 {
		t.Fatalf("difat sectors = %v", got)
	}
}

// TestLoadDirectoryEmpty covers the empty-directory corruption exit.
func TestLoadDirectoryEmpty(t *testing.T) {
	file := &compoundFile{
		data: make([]byte, cfbHeaderSize+512),
		fat:  []uint32{cfbEndOfChain},
	}
	binary.LittleEndian.PutUint32(file.data[0x30:], cfbEndOfChain) // no directory chain.
	if err := file.loadDirectory(); !errors.Is(err, errCompoundCorrupt) {
		t.Fatalf("empty directory err = %v", err)
	}
}

// TestDecodeDirEntryClampsName covers the over-long name-length clamp.
func TestDecodeDirEntryClampsName(t *testing.T) {
	raw := make([]byte, cfbDirEntrySize)
	for i := 0; i < 64; i += 2 {
		binary.LittleEndian.PutUint16(raw[i:], uint16('a'))
	}
	binary.LittleEndian.PutUint16(raw[0x40:], 0x00FF) // absurd name length.
	raw[0x42] = 2
	entry := decodeDirEntry(raw)
	if len(entry.name) != 32 {
		t.Fatalf("clamped name length = %d", len(entry.name))
	}
}

// TestRootEntryAbsent covers rootEntry returning false.
func TestRootEntryAbsent(t *testing.T) {
	file := &compoundFile{entries: []cfbEntry{{name: "S", kind: 2}}}
	if _, ok := file.rootEntry(); ok {
		t.Fatal("no root entry should be reported")
	}
}

// TestReadChainCapsHugeSize covers the size clamp on the regular read path.
func TestReadChainCapsHugeSize(t *testing.T) {
	body := buildCompoundFile([]cfbStream{{name: "Big", data: []byte("hello")}})
	file, err := openCompoundFile(body)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if got := file.readChain(0, cfbMaxStream+1); len(got) > cfbMaxStream {
		t.Fatalf("readChain did not cap: %d", len(got))
	}
}

// TestReadMiniChainCapsHugeSize covers the size clamp on the mini path.
func TestReadMiniChainCapsHugeSize(t *testing.T) {
	body := buildCompoundFile([]cfbStream{{name: "Tiny", data: []byte("mini body")}})
	file, err := openCompoundFile(body)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if got := file.readMiniChain(0, cfbMaxStream+1); len(got) > cfbMaxStream {
		t.Fatalf("readMiniChain did not cap: %d", len(got))
	}
}

// TestMetadataStreamClassification covers each machinery-stream case.
func TestMetadataStreamClassification(t *testing.T) {
	metadata := []string{"", "\x01Ole", "\x05SummaryInformation", "CompObj", "ObjInfo"}
	for _, name := range metadata {
		if !metadataStream(name) {
			t.Fatalf("%q should be classified as metadata", name)
		}
	}
	for _, name := range []string{"WordDocument", "VisioDocument", "Workbook"} {
		if metadataStream(name) {
			t.Fatalf("%q should be content", name)
		}
	}
}

// TestSharedStringsSpanContinueRecord drives the SST across a real CONTINUE
// record boundary through the record-level entry point.
func TestSharedStringsSpanContinueRecord(t *testing.T) {
	sst := le32(nil, 1)                 // cstTotal.
	sst = le32(sst, 1)                  // cstUnique.
	sst = append(sst, 0x08, 0x00, 0x00) // cch=8, compressed.
	sst = append(sst, []byte("Cont")...)
	records := []biffRecord{
		{recType: 0x00FC, data: sst},
		{recType: 0x003C, data: append([]byte{0x00}, []byte("inue")...)},
	}
	got := sharedStrings(records)
	if len(got) != 1 || got[0] != "Continue" {
		t.Fatalf("continue-spanning SST = %#v", got)
	}
}
