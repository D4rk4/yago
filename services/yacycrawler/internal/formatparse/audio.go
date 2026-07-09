package formatparse

import (
	"bytes"
	"encoding/binary"
	"strings"
)

// parseAudio indexes audio tags and metadata — title, artist, album, genre,
// comments — never the audio content (no transcription, matching YaCy). Each
// container gets its own bounded tag reader; the remaining formats fall back
// to readable-run extraction.
func parseAudio(rawURL, _ string, body []byte) (Document, bool) {
	lines := audioTagLines(urlExtension(rawURL), body)
	if len(lines) == 0 {
		return Document{URL: rawURL}, false
	}
	text := strings.Join(lines, "\n")
	title := audioTitle(lines)
	if title == "" {
		title = imageTitle(rawURL)
	}

	return Document{URL: rawURL, Title: title, Text: text}, true
}

func audioTagLines(ext string, body []byte) []string {
	switch ext {
	case "mp3":
		return append(id3v2Lines(body), id3v1Lines(body)...)
	case "ogg":
		return vorbisCommentLines(body)
	case "wav", "aif", "aifc", "aiff":
		return riffInfoLines(body)
	case "m4a", "m4b", "m4p", "mp4":
		return mp4TagLines(body)
	case "sid":
		return sidLines(body)
	default:
		// wma/ra/rm: best-effort readable runs from the container header.
		return clipLines(printableRuns(clipHead(body)), 8)
	}
}

// audioTitle prefers the line tagged as the title.
func audioTitle(lines []string) string {
	for _, line := range lines {
		if value, found := strings.CutPrefix(line, "Title: "); found {
			return value
		}
	}

	return ""
}

// id3v2TextFrames maps the ID3v2.3/2.4 frames worth indexing.
var id3v2TextFrames = map[string]string{
	"TIT2": "Title", "TPE1": "Artist", "TALB": "Album", "TCON": "Genre",
	"TYER": "Year", "TDRC": "Year", "COMM": "Comment", "USLT": "Lyrics",
}

// id3v2Lines walks the ID3v2 frames at the head of an MP3.
func id3v2Lines(body []byte) []string {
	if len(body) < 10 || !bytes.HasPrefix(body, []byte("ID3")) {
		return nil
	}
	size := syncsafe(body[6:10])
	end := 10 + size
	if end > len(body) {
		end = len(body)
	}
	lines := make([]string, 0, 4)
	at := 10
	for at+10 <= end {
		name := string(body[at : at+4])
		frameSize := int(binary.BigEndian.Uint32(body[at+4 : at+8]))
		if name == "\x00\x00\x00\x00" || frameSize <= 0 || at+10+frameSize > end {
			break
		}
		if label, ok := id3v2TextFrames[name]; ok {
			if value := id3TextValue(body[at+10 : at+10+frameSize]); value != "" {
				lines = append(lines, label+": "+value)
			}
		}
		at += 10 + frameSize
	}

	return lines
}

// id3TextValue decodes a text frame payload: an encoding byte then the value.
func id3TextValue(payload []byte) string {
	if len(payload) < 2 {
		return ""
	}
	encoding := payload[0]
	value := payload[1:]
	if encoding == 1 || encoding == 2 {
		return strings.TrimSpace(utf16LEString(value))
	}

	return strings.TrimSpace(strings.Trim(string(value), "\x00"))
}

// utf16LEString decodes UTF-16 text with an optional BOM (ASCII subset).
func utf16LEString(value []byte) string {
	if len(value) >= 2 &&
		(value[0] == 0xFF && value[1] == 0xFE || value[0] == 0xFE && value[1] == 0xFF) {
		value = value[2:]
	}
	var out strings.Builder
	for i := 0; i+1 < len(value); i += 2 {
		if value[i] != 0 && value[i+1] == 0 && printableASCII(value[i]) {
			out.WriteByte(value[i])
		}
	}

	return out.String()
}

func syncsafe(raw []byte) int {
	return int(raw[0])<<21 | int(raw[1])<<14 | int(raw[2])<<7 | int(raw[3])
}

// id3v1Lines reads the fixed 128-byte TAG trailer.
func id3v1Lines(body []byte) []string {
	if len(body) < 128 {
		return nil
	}
	tag := body[len(body)-128:]
	if !bytes.HasPrefix(tag, []byte("TAG")) {
		return nil
	}
	lines := make([]string, 0, 3)
	appendTagged := func(label string, raw []byte) {
		if value := strings.TrimSpace(strings.Trim(string(raw), "\x00")); value != "" {
			lines = append(lines, label+": "+value)
		}
	}
	appendTagged("Title", tag[3:33])
	appendTagged("Artist", tag[33:63])
	appendTagged("Album", tag[63:93])

	return lines
}

const vorbisMaxComments = 32

// vorbisCommentLines finds the Vorbis comment header inside an Ogg stream.
func vorbisCommentLines(body []byte) []string {
	marker := append([]byte{3}, []byte("vorbis")...)
	index := bytes.Index(body, marker)
	if index < 0 {
		return nil
	}
	at := index + len(marker)
	vendorLen, ok := readUint32LE(body, at)
	if !ok || at+4+vendorLen > len(body) {
		return nil
	}
	at += 4 + vendorLen
	count, ok := readUint32LE(body, at)
	if !ok {
		return nil
	}
	at += 4
	lines := make([]string, 0, 4)
	for i := 0; i < count && i < vorbisMaxComments; i++ {
		length, ok := readUint32LE(body, at)
		if !ok || at+4+length > len(body) {
			break
		}
		comment := string(body[at+4 : at+4+length])
		at += 4 + length
		if key, value, found := strings.Cut(comment, "="); found && value != "" {
			lines = append(lines, vorbisLabel(key)+": "+value)
		}
	}

	return lines
}

func vorbisLabel(key string) string {
	key = strings.ToUpper(key)
	switch key {
	case "TITLE":
		return "Title"
	case "ARTIST":
		return "Artist"
	case "ALBUM":
		return "Album"
	case "GENRE":
		return "Genre"
	default:
		return strings.Title(strings.ToLower(key)) //nolint:staticcheck // ASCII vorbis keys only.
	}
}

func readUint32LE(body []byte, at int) (int, bool) {
	if at < 0 || at+4 > len(body) {
		return 0, false
	}

	return int(binary.LittleEndian.Uint32(body[at : at+4])), true
}

// riffInfoTags maps RIFF LIST INFO and AIFF chunk ids to labels.
var riffInfoTags = map[string]string{
	"INAM": "Title", "IART": "Artist", "IPRD": "Album", "IGNR": "Genre",
	"ICMT": "Comment", "NAME": "Title", "AUTH": "Artist", "ANNO": "Comment",
}

// riffInfoLines scans WAV/AIFF chunk ids for the INFO/annotation chunks.
func riffInfoLines(body []byte) []string {
	lines := make([]string, 0, 4)
	for id, label := range riffInfoTags {
		index := bytes.Index(body, []byte(id))
		if index < 0 || index+8 > len(body) {
			continue
		}
		size := int(binary.LittleEndian.Uint32(body[index+4 : index+8]))
		if id == "NAME" || id == "AUTH" || id == "ANNO" {
			size = int(binary.BigEndian.Uint32(body[index+4 : index+8]))
		}
		end := index + 8 + size
		if size <= 0 || end > len(body) {
			continue
		}
		value := strings.TrimSpace(strings.Trim(string(body[index+8:end]), "\x00"))
		if value != "" {
			lines = append(lines, label+": "+value)
		}
	}
	sortLines(lines)

	return lines
}

// mp4TagAtoms maps iTunes-style ilst atoms to labels.
var mp4TagAtoms = map[string]string{
	"\xa9nam": "Title", "\xa9ART": "Artist", "\xa9alb": "Album", "\xa9gen": "Genre",
}

// mp4TagLines scans for iTunes metadata atoms: [size][name][data atom] with
// the value after the 16-byte data-atom header.
func mp4TagLines(body []byte) []string {
	lines := make([]string, 0, 4)
	for atom, label := range mp4TagAtoms {
		index := bytes.Index(body, []byte(atom))
		if index < 0 || index < 4 {
			continue
		}
		size := int(binary.BigEndian.Uint32(body[index-4 : index]))
		end := index - 4 + size
		if size <= 24 || end > len(body) {
			continue
		}
		value := strings.TrimSpace(strings.Trim(string(body[index+20:end]), "\x00"))
		if value != "" {
			lines = append(lines, label+": "+value)
		}
	}
	sortLines(lines)

	return lines
}

// sidLines reads the fixed PSID/RSID header strings.
func sidLines(body []byte) []string {
	if len(body) < 0x76 ||
		!bytes.HasPrefix(body, []byte("PSID")) && !bytes.HasPrefix(body, []byte("RSID")) {
		return nil
	}
	lines := make([]string, 0, 3)
	appendField := func(label string, raw []byte) {
		if value := strings.TrimSpace(strings.Trim(string(raw), "\x00")); value != "" {
			lines = append(lines, label+": "+value)
		}
	}
	appendField("Title", body[0x16:0x36])
	appendField("Artist", body[0x36:0x56])
	appendField("Comment", body[0x56:0x76])

	return lines
}

func clipLines(lines []string, limit int) []string {
	if len(lines) > limit {
		return lines[:limit]
	}

	return lines
}

// sortLines orders map-derived lines deterministically, titles first.
func sortLines(lines []string) {
	for i := 1; i < len(lines); i++ {
		for j := i; j > 0 && lineRank(lines[j]) < lineRank(lines[j-1]); j-- {
			lines[j], lines[j-1] = lines[j-1], lines[j]
		}
	}
}

func lineRank(line string) string {
	order := map[string]string{
		"Title":   "0",
		"Artist":  "1",
		"Album":   "2",
		"Genre":   "3",
		"Comment": "4",
	}
	label, _, _ := strings.Cut(line, ":")
	if rank, ok := order[label]; ok {
		return rank + line
	}

	return "9" + line
}
