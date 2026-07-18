package formatparse

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

// len32 bounds a test length into a uint32 field.
func len32(n int) uint32 {
	if n < 0 || n > 0xFFFF {
		panic("test payload too large")
	}

	return uint32(n)
}

func id3v2Frame(name, value string) []byte {
	payload := make([]byte, 0, 1+len(value))
	payload = append(payload, 0)
	payload = append(payload, []byte(value)...)
	frame := []byte(name)
	frame = binary.BigEndian.AppendUint32(frame, len32(len(payload)))
	frame = append(frame, 0, 0)

	return append(frame, payload...)
}

// id3v2Header builds the 10-byte ID3v2 header with a syncsafe size.
func id3v2Header(size int) []byte {
	header := []byte("ID3\x04\x00\x00")

	return append(header,
		byte(size>>21&0x7f), byte(size>>14&0x7f), byte(size>>7&0x7f), byte(size&0x7f))
}

func mp3WithTags(t *testing.T) []byte {
	t.Helper()
	frames := id3v2Frame("TIT2", "Night Drive")
	frames = append(frames, id3v2Frame("TPE1", "The Waves")...)
	frames = append(frames, id3v2Frame("TALB", "Roads")...)
	header := id3v2Header(len(frames))
	body := make([]byte, 0, len(header)+len(frames)+64+128)
	body = append(body, header...)
	body = append(body, frames...)
	body = append(body, make([]byte, 64)...)

	tag := make([]byte, 128)
	copy(tag, "TAG")
	copy(tag[3:], "V1 Title")
	copy(tag[33:], "V1 Artist")

	return append(body, tag...)
}

func TestParseMP3Tags(t *testing.T) {
	page, parsed := Parse(
		"https://a.example/track.mp3", "audio/mpeg", mp3WithTags(t),
		yagocrawlcontract.DefaultFormatToggles(),
	)
	if !parsed || page.Title != "Night Drive" {
		t.Fatalf("mp3 parse = %v %+v", parsed, page)
	}
	for _, want := range []string{
		"Artist: The Waves", "Album: Roads", "Title: V1 Title", "Artist: V1 Artist",
	} {
		if !strings.Contains(page.Text, want) {
			t.Fatalf("mp3 missing %q in %q", want, page.Text)
		}
	}
}

func TestParseVorbisComments(t *testing.T) {
	var ogg bytes.Buffer
	ogg.WriteString("OggS junk ")
	ogg.WriteByte(3)
	ogg.WriteString("vorbis")
	vendor := "yago-test"
	_ = binary.Write(&ogg, binary.LittleEndian, len32(len(vendor)))
	ogg.WriteString(vendor)
	comments := []string{"TITLE=Sea Song", "ARTIST=Blue Choir", "CUSTOMKEY=Some value", "BROKEN"}
	_ = binary.Write(&ogg, binary.LittleEndian, len32(len(comments)))
	for _, comment := range comments {
		_ = binary.Write(&ogg, binary.LittleEndian, len32(len(comment)))
		ogg.WriteString(comment)
	}

	page, parsed := Parse(
		"https://a.example/song.ogg", "audio/ogg", ogg.Bytes(),
		yagocrawlcontract.DefaultFormatToggles(),
	)
	if !parsed || page.Title != "Sea Song" {
		t.Fatalf("ogg parse = %v %+v", parsed, page)
	}
	for _, want := range []string{"Artist: Blue Choir", "Customkey: Some value"} {
		if !strings.Contains(page.Text, want) {
			t.Fatalf("ogg missing %q in %q", want, page.Text)
		}
	}
}

func TestParseRIFFAndAIFF(t *testing.T) {
	wav := []byte("RIFF????WAVELIST????INFOINAM")
	wav = binary.LittleEndian.AppendUint32(wav, 9)
	wav = append(wav, []byte("Wave song")...)
	wav = append(wav, []byte("IART")...)
	wav = binary.LittleEndian.AppendUint32(wav, 6)
	wav = append(wav, []byte("Wavers")...)
	page, parsed := Parse(
		"https://a.example/clip.wav", "audio/wav", wav,
		yagocrawlcontract.DefaultFormatToggles(),
	)
	if !parsed || page.Title != "Wave song" || !strings.Contains(page.Text, "Artist: Wavers") {
		t.Fatalf("wav parse = %v %+v", parsed, page)
	}

	aiff := []byte("FORM????AIFFNAME")
	aiff = binary.BigEndian.AppendUint32(aiff, 8)
	aiff = append(aiff, []byte("Air song")...)
	page, parsed = Parse(
		"https://a.example/clip.aiff", "audio/aiff", aiff,
		yagocrawlcontract.DefaultFormatToggles(),
	)
	if !parsed || page.Title != "Air song" {
		t.Fatalf("aiff parse = %v %+v", parsed, page)
	}
}

func mp4Atom(name, value string) []byte {
	inner := make([]byte, 16, 16+len(value))
	copy(inner[4:], "data")
	inner = append(inner, []byte(value)...)
	atom := binary.BigEndian.AppendUint32(nil, len32(8+len(inner)))
	atom = append(atom, []byte(name)...)

	return append(atom, inner...)
}

func TestParseMP4AndSIDAndFallback(t *testing.T) {
	toggles := yagocrawlcontract.DefaultFormatToggles()
	m4a := []byte("....ftypM4A ")
	m4a = append(m4a, mp4Atom("\xa9nam", "Air Waves")...)
	m4a = append(m4a, mp4Atom("\xa9ART", "M Artist")...)
	page, parsed := Parse("https://a.example/track.m4a", "audio/mp4", m4a, toggles)
	if !parsed || page.Title != "Air Waves" || !strings.Contains(page.Text, "Artist: M Artist") {
		t.Fatalf("m4a parse = %v %+v", parsed, page)
	}

	sid := make([]byte, 0x80)
	copy(sid, "PSID")
	copy(sid[0x16:], "Last Ninja")
	copy(sid[0x36:], "Composer X")
	copy(sid[0x56:], "1988 Games")
	page, parsed = Parse("https://a.example/tune.sid", "audio/prs.sid", sid, toggles)
	if !parsed || page.Title != "Last Ninja" ||
		!strings.Contains(page.Text, "Comment: 1988 Games") {
		t.Fatalf("sid parse = %v %+v", parsed, page)
	}

	wma := append([]byte{0x30, 0x26}, []byte("Windows Media Audio stream title here")...)
	page, parsed = Parse("https://a.example/clip.wma", "audio/x-ms-wma", wma, toggles)
	if !parsed || !strings.Contains(page.Text, "Windows Media Audio stream title here") {
		t.Fatalf("wma parse = %v %+v", parsed, page)
	}
	if page.Title != "clip.wma" {
		t.Fatalf("fallback title = %q", page.Title)
	}

	if _, parsed := Parse(
		"https://a.example/silent.mp3",
		"audio/mpeg",
		[]byte{0, 1, 2},
		toggles,
	); parsed {
		t.Fatal("tagless audio must stay unparsed")
	}
}

func TestAudioEdgeBranches(t *testing.T) {
	// Truncated ID3v2: declared size beyond the body ends the walk.
	head := []byte("ID3\x04\x00\x00\x00\x00\x00\x7f")
	head = append(head, id3v2Frame("TIT2", "x")...)
	if lines := id3v2Lines(head[:12]); len(lines) != 0 {
		t.Fatalf("truncated id3 = %v", lines)
	}
	// UTF-16 text frame decodes through the wide path.
	payload := []byte{1, 0xFF, 0xFE, 'W', 0, 'i', 0, 'd', 0, 'e', 0}
	if got := id3TextValue(payload); got != "Wide" {
		t.Fatalf("utf16 frame = %q", got)
	}
	if got := id3TextValue([]byte{0}); got != "" {
		t.Fatalf("short frame = %q", got)
	}
	// Vorbis header with a truncated vendor or count yields nothing.
	marker := append([]byte{3}, []byte("vorbis")...)
	if lines := vorbisCommentLines(append(marker, 0xFF, 0xFF)); lines != nil {
		t.Fatalf("truncated vorbis = %v", lines)
	}
	noCount := append([]byte{}, marker...)
	noCount = binary.LittleEndian.AppendUint32(noCount, 0)
	if lines := vorbisCommentLines(noCount); lines != nil {
		t.Fatalf("countless vorbis = %v", lines)
	}
	// A comment whose declared length overruns stops the loop.
	overrun := append([]byte{}, marker...)
	overrun = binary.LittleEndian.AppendUint32(overrun, 0)
	overrun = binary.LittleEndian.AppendUint32(overrun, 2)
	overrun = binary.LittleEndian.AppendUint32(overrun, 99)
	if lines := vorbisCommentLines(overrun); len(lines) != 0 {
		t.Fatalf("overrun vorbis = %v", lines)
	}
	// SID shorter than its header yields nothing.
	if lines := sidLines([]byte("PSID")); lines != nil {
		t.Fatalf("short sid = %v", lines)
	}
	// RIFF chunk with an overrunning size skips.
	bad := []byte("INAM")
	bad = binary.LittleEndian.AppendUint32(bad, 99)
	if lines := riffInfoLines(bad); len(lines) != 0 {
		t.Fatalf("overrun riff = %v", lines)
	}
	// MP4 atom with a nonsense size skips.
	tiny := append(binary.BigEndian.AppendUint32(nil, 5), []byte("\xa9nam")...)
	if lines := mp4TagLines(tiny); len(lines) != 0 {
		t.Fatalf("tiny mp4 atom = %v", lines)
	}
}

func TestAudioFinalBranches(t *testing.T) {
	// Padding frame ends the ID3v2 walk.
	frames := append(id3v2Frame("TIT2", "Padded"), 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0)
	header := id3v2Header(len(frames))
	if lines := id3v2Lines(append(header, frames...)); len(lines) != 1 {
		t.Fatalf("padded id3 = %v", lines)
	}

	// A file with 128+ bytes but no TAG trailer yields no v1 lines.
	if lines := id3v1Lines(make([]byte, 200)); lines != nil {
		t.Fatalf("untagged v1 = %v", lines)
	}

	// Ogg without a vorbis comment header yields nothing.
	if lines := vorbisCommentLines([]byte("OggS but no comments")); lines != nil {
		t.Fatalf("vorbis-free ogg = %v", lines)
	}

	// Album and genre labels map through vorbisLabel.
	if vorbisLabel("ALBUM") != "Album" || vorbisLabel("GENRE") != "Genre" {
		t.Fatal("vorbis label mapping broken")
	}

	// The fallback run extraction clips to its line limit.
	long := bytes.Repeat([]byte("word \x01"), 40)
	if lines := clipLines(printableRuns(long), 8); len(lines) != 8 {
		t.Fatalf("clip = %d", len(lines))
	}

	// Title ordering keys rank known labels ahead of unknown ones.
	lines := []string{"Zother: x", "Title: y"}
	sortLines(lines)
	if lines[0] != "Title: y" {
		t.Fatalf("sort = %v", lines)
	}
}
