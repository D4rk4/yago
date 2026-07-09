package formatparse

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"image"
	"image/png"
	"strings"
	"testing"
)

// exifTIFFHex is a little-endian TIFF EXIF payload: Make=Yago, Model=Cam-1,
// DateTime=2026:07:06 10:00:00, GPS 48°30'N 2°15'W.
const exifTIFFHex = "49492a000800000004000f010200050000007400000010010200060000007900000032010200140000007f00000025880400010000003e00000000000000040001000200020000004e0000000200050003000000930000000300020002000000570000000400050003000000ab000000000000005961676f0043616d2d3100323032363a30373a30362031303a30303a30300030000000010000001e00000001000000000000000100000002000000010000000f000000010000000000000001000000"

// segmentLen16 bounds a test segment length into the JPEG 16-bit size field.
func segmentLen16(n int) uint16 {
	if n < 0 || n > 0xFFFF {
		panic("test segment too large")
	}

	return uint16(n)
}

func pngImage(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 12, 7))); err != nil {
		t.Fatalf("png: %v", err)
	}

	return buf.Bytes()
}

func jpegWithEXIF(t *testing.T) []byte {
	t.Helper()
	tiff, err := hex.DecodeString(exifTIFFHex)
	if err != nil {
		t.Fatalf("hex: %v", err)
	}
	payload := append([]byte("Exif\x00\x00"), tiff...)
	app1 := []byte{0xFF, 0xE1}
	app1 = binary.BigEndian.AppendUint16(app1, segmentLen16(len(payload)+2))
	app1 = append(app1, payload...)
	body := make([]byte, 0, 4+len(app1))
	body = append(body, 0xFF, 0xD8)
	body = append(body, app1...)
	body = append(body, 0xFF, 0xD9)

	return body
}

func TestParseImageDimensionsAndVerticalPayload(t *testing.T) {
	page, parsed := Parse(
		"https://a.example/photos/pic.png?s=1", "image/png", pngImage(t),
		DefaultToggles(),
	)
	if !parsed || !strings.Contains(page.Text, "PNG 12x7") {
		t.Fatalf("png parse = %v %+v", parsed, page)
	}
	if page.Title != "pic.png" {
		t.Fatalf("image title = %q", page.Title)
	}
	if len(page.Images) != 1 || page.Images[0].URL != "https://a.example/photos/pic.png?s=1" {
		t.Fatalf("image payload = %+v", page.Images)
	}
}

func TestParseImageEXIF(t *testing.T) {
	page, parsed := Parse(
		"https://a.example/shot.jpg", "image/jpeg", jpegWithEXIF(t),
		DefaultToggles(),
	)
	if !parsed {
		t.Fatal("jpeg with EXIF must parse")
	}
	for _, want := range []string{
		"Camera: Yago Cam-1", "Taken: 2026:07:06 10:00:00", "GPS: 48.500000, -2.250000",
	} {
		if !strings.Contains(page.Text, want) {
			t.Fatalf("exif missing %q in %q", want, page.Text)
		}
	}
}

func TestParseImageRawHeadersAndSVG(t *testing.T) {
	toggles := DefaultToggles()

	bmp := make([]byte, 26)
	copy(bmp, "BM")
	binary.LittleEndian.PutUint32(bmp[18:], 640)
	binary.LittleEndian.PutUint32(bmp[22:], 480)
	page, parsed := Parse("https://a.example/x.bmp", "image/bmp", bmp, toggles)
	if !parsed || !strings.Contains(page.Text, "BMP 640x480") {
		t.Fatalf("bmp = %v %+v", parsed, page)
	}

	psd := make([]byte, 26)
	copy(psd, "8BPS")
	binary.BigEndian.PutUint32(psd[14:], 300)
	binary.BigEndian.PutUint32(psd[18:], 200)
	page, parsed = Parse("https://a.example/x.psd", "image/vnd.adobe.photoshop", psd, toggles)
	if !parsed || !strings.Contains(page.Text, "PSD 200x300") {
		t.Fatalf("psd = %v %+v", parsed, page)
	}

	page, parsed = Parse(
		"https://a.example/x.wbmp",
		"image/vnd.wap.wbmp",
		[]byte{0, 0, 32, 16},
		toggles,
	)
	if !parsed || !strings.Contains(page.Text, "WBMP 32x16") {
		t.Fatalf("wbmp = %v %+v", parsed, page)
	}

	svg := `<svg xmlns="http://www.w3.org/2000/svg" width="24" height="12">` +
		`<title>Logo mark</title><desc>Company sign</desc></svg>`
	page, parsed = Parse("https://a.example/logo.svg", "image/svg+xml", []byte(svg), toggles)
	if !parsed || page.Title != "Logo mark" ||
		!strings.Contains(page.Text, "Company sign") || !strings.Contains(page.Text, "SVG 24x12") {
		t.Fatalf("svg = %v %+v", parsed, page)
	}

	if _, parsed := Parse(
		"https://a.example/bare.svg", "image/svg+xml", []byte(`<svg xmlns="x"/>`), toggles,
	); parsed {
		t.Fatal("metadata-free svg must stay unparsed")
	}
	if _, parsed := Parse(
		"https://a.example/broken.svg", "image/svg+xml", []byte(`<svg`), toggles,
	); parsed {
		t.Fatal("malformed svg must stay unparsed")
	}
	if _, parsed := Parse(
		"https://a.example/junk.gif", "image/gif", []byte{1, 2, 3}, toggles,
	); parsed {
		t.Fatal("undecodable image must stay unparsed")
	}
}

func TestEXIFEdges(t *testing.T) {
	if lines := jpegEXIFLines(pngImage(t)); lines != nil {
		t.Fatalf("png exif = %v", lines)
	}
	if lines := jpegEXIFLines([]byte{0xFF, 0xD8, 0xFF, 0xE1, 0x00, 0x04, 'J', 'F'}); lines != nil {
		t.Fatalf("non-exif app1 = %v", lines)
	}
	truncated := jpegWithEXIF(t)[:20]
	if lines := jpegEXIFLines(truncated); len(lines) != 0 {
		t.Fatalf("truncated exif = %v", lines)
	}
	if order, _ := tiffLayout([]byte("XX000000")); order != nil {
		t.Fatal("bad byte order must be rejected")
	}
	if got := gpsLine([]byte{}, 0, binary.LittleEndian); got != "" {
		t.Fatalf("empty gps ifd = %q", got)
	}
	if got := joinSpace(nil); got != "" {
		t.Fatalf("joinSpace nil = %q", got)
	}

	// A bad-order TIFF inside a JPEG covers the layout rejection path.
	badOrder := append([]byte("Exif\x00\x00XX000000"), 0)
	app1 := []byte{0xFF, 0xE1}
	app1 = binary.BigEndian.AppendUint16(app1, segmentLen16(len(badOrder)+2))
	jpeg := make([]byte, 0, 2+len(app1)+len(badOrder))
	jpeg = append(jpeg, 0xFF, 0xD8)
	jpeg = append(jpeg, app1...)
	jpeg = append(jpeg, badOrder...)
	if lines := jpegEXIFLines(jpeg); lines != nil {
		t.Fatalf("bad byte order exif = %v", lines)
	}

	// Big-endian TIFF header exercises the MM branch.
	if order, _ := tiffLayout([]byte("MM\x00*\x00\x00\x00\x08")); order != binary.BigEndian {
		t.Fatal("MM order must decode big-endian")
	}

	// A JPEG with a non-Exif APP1 before the Exif one covers the scan loop.
	real := jpegWithEXIF(t)
	prefix := []byte{0xFF, 0xD8, 0xFF, 0xE1, 0x00, 0x04, 'J', 'F'}
	scan := make([]byte, 0, len(prefix)+len(real))
	scan = append(scan, prefix...)
	scan = append(scan, real[2:]...)
	if lines := jpegEXIFLines(scan); len(lines) == 0 {
		t.Fatalf("exif after non-exif app1 must decode: %v", lines)
	}

	// An IFD whose offset points beyond the payload yields no fields.
	empty := readIFD([]byte("II*\x00"), 99, binary.LittleEndian)
	if len(empty.text)+len(empty.offsets)+len(empty.rationals) != 0 {
		t.Fatalf("out-of-range ifd = %+v", empty)
	}

	// Inline ASCII values (length <= 4) decode from the entry itself.
	inline := make([]byte, 0, 64)
	inline = append(inline, 'I', 'I', '*', 0)
	inline = binary.LittleEndian.AppendUint32(inline, 8)
	inline = binary.LittleEndian.AppendUint16(inline, 1)
	inline = binary.LittleEndian.AppendUint16(inline, exifTagDateTime)
	inline = binary.LittleEndian.AppendUint16(inline, exifTypeASCII)
	inline = binary.LittleEndian.AppendUint32(inline, 3)
	inline = append(inline, 'h', 'i', 0, 0)
	inline = binary.LittleEndian.AppendUint32(inline, 0)
	fields := readIFD(inline, 8, binary.LittleEndian)
	if fields.text[exifTagDateTime] != "hi" {
		t.Fatalf("inline ascii = %+v", fields.text)
	}

	// GPS rationals with a truncated value table stop early; a zero
	// denominator contributes zero.
	gps := gpsLine([]byte("II*\x00\x00\x00"), 0, binary.LittleEndian)
	if gps != "" {
		t.Fatalf("truncated gps = %q", gps)
	}
}

func TestEXIFRemainingBranches(t *testing.T) {
	// One APP1 that is not Exif, long enough to scan, with no further APP1.
	scanEnd := make([]byte, 0, 22)
	scanEnd = append(scanEnd, 0xFF, 0xD8, 0xFF, 0xE1, 0x00, 0x10)
	scanEnd = append(scanEnd, []byte("JFIFdatadatadata")...)
	if lines := jpegEXIFLines(scanEnd); lines != nil {
		t.Fatalf("app1 scan without exif = %v", lines)
	}

	if order, _ := tiffLayout([]byte("II")); order != nil {
		t.Fatal("short tiff header must be rejected")
	}

	// A rational entry whose value table sits out of range stops early.
	short := make([]byte, 0, 32)
	short = append(short, 'I', 'I', '*', 0)
	short = binary.LittleEndian.AppendUint32(short, 8)
	short = binary.LittleEndian.AppendUint16(short, 1)
	short = binary.LittleEndian.AppendUint16(short, gpsTagLat)
	short = binary.LittleEndian.AppendUint16(short, exifTypeRational)
	short = binary.LittleEndian.AppendUint32(short, 1)
	short = binary.LittleEndian.AppendUint32(short, 999)
	short = binary.LittleEndian.AppendUint32(short, 0)
	fields := readIFD(short, 8, binary.LittleEndian)
	if fields.rationals[gpsTagLat] != [3]float64{} {
		t.Fatalf("out-of-range rational = %+v", fields.rationals)
	}

	// Southern latitude flips the sign; a single camera part joins alone.
	tiff, err := hex.DecodeString(exifTIFFHex)
	if err != nil {
		t.Fatalf("hex: %v", err)
	}
	south := bytes.Replace(tiff, []byte{'N', 0, 0, 0}, []byte{'S', 0, 0, 0}, 1)
	order, ifd0 := tiffLayout(south)
	gpsFields := readIFD(south, ifd0, order)
	if got := gpsLine(
		south,
		gpsFields.offsets[exifTagGPSIFD],
		order,
	); !strings.HasPrefix(
		got,
		"GPS: -48.5",
	) {
		t.Fatalf("southern gps = %q", got)
	}
	if got := joinNonEmpty("OnlyMake", ""); got != "OnlyMake" {
		t.Fatalf("single join = %q", got)
	}
}

func TestSVGWithoutTitleAndBigHead(t *testing.T) {
	toggles := DefaultToggles()
	svg := `<svg xmlns="http://www.w3.org/2000/svg"><desc>Only description</desc></svg>`
	page, parsed := Parse("https://a.example/plain.svg", "image/svg+xml", []byte(svg), toggles)
	if !parsed || page.Title != "plain.svg" {
		t.Fatalf("titleless svg = %v %+v", parsed, page)
	}

	big := make([]byte, 600)
	copy(big, "BM")
	binary.LittleEndian.PutUint32(big[18:], 10)
	binary.LittleEndian.PutUint32(big[22:], 20)
	if _, parsed := Parse("https://a.example/big.bmp", "image/bmp", big, toggles); !parsed {
		t.Fatal("large bmp must parse")
	}
}
