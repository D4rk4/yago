package formatparse

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

const (
	exifTagMake     = 0x010F
	exifTagModel    = 0x0110
	exifTagDateTime = 0x0132
	exifTagGPSIFD   = 0x8825

	gpsTagLatRef = 0x0001
	gpsTagLat    = 0x0002
	gpsTagLonRef = 0x0003
	gpsTagLon    = 0x0004

	exifTypeASCII    = 2
	exifTypeRational = 5
)

// jpegEXIFLines extracts the capture metadata a search index cares about from
// a JPEG's EXIF segment: camera make/model, capture time, and GPS position.
// Anything malformed simply yields no lines.
func jpegEXIFLines(body []byte) []string {
	tiff := exifSegment(body)
	if tiff == nil {
		return nil
	}
	order, ifd0 := tiffLayout(tiff)
	if order == nil {
		return nil
	}
	fields := readIFD(tiff, ifd0, order)
	lines := make([]string, 0, 3)
	if camera := joinNonEmpty(fields.text[exifTagMake], fields.text[exifTagModel]); camera != "" {
		lines = append(lines, "Camera: "+camera)
	}
	if taken := fields.text[exifTagDateTime]; taken != "" {
		lines = append(lines, "Taken: "+taken)
	}
	if gpsOffset, ok := fields.offsets[exifTagGPSIFD]; ok {
		if position := gpsLine(tiff, gpsOffset, order); position != "" {
			lines = append(lines, position)
		}
	}

	return lines
}

// exifSegment finds the APP1 Exif payload and returns its TIFF body.
func exifSegment(body []byte) []byte {
	if len(body) < 4 || body[0] != 0xFF || body[1] != 0xD8 {
		return nil
	}
	marker := []byte{0xFF, 0xE1}
	index := bytes.Index(body, marker)
	for index >= 0 && index+10 < len(body) {
		if bytes.Equal(body[index+4:index+10], []byte("Exif\x00\x00")) {
			size := int(binary.BigEndian.Uint16(body[index+2 : index+4]))
			end := index + 2 + size
			if end > len(body) {
				end = len(body)
			}

			return body[index+10 : end]
		}
		next := bytes.Index(body[index+2:], marker)
		if next < 0 {
			return nil
		}
		index += 2 + next
	}

	return nil
}

// tiffLayout reads the TIFF header: byte order and first IFD offset.
func tiffLayout(tiff []byte) (binary.ByteOrder, int) {
	if len(tiff) < 8 {
		return nil, 0
	}
	var order binary.ByteOrder
	switch {
	case tiff[0] == 'I' && tiff[1] == 'I':
		order = binary.LittleEndian
	case tiff[0] == 'M' && tiff[1] == 'M':
		order = binary.BigEndian
	default:
		return nil, 0
	}

	return order, int(order.Uint32(tiff[4:8]))
}

// ifdFields carries the decoded entries of one IFD.
type ifdFields struct {
	text    map[uint16]string
	offsets map[uint16]int
	// rationals holds the first three rational values per tag (degrees,
	// minutes, seconds for the GPS coordinates).
	rationals map[uint16][3]float64
}

func readIFD(tiff []byte, offset int, order binary.ByteOrder) ifdFields {
	fields := ifdFields{
		text:      map[uint16]string{},
		offsets:   map[uint16]int{},
		rationals: map[uint16][3]float64{},
	}
	if offset < 0 || offset+2 > len(tiff) {
		return fields
	}
	count := int(order.Uint16(tiff[offset : offset+2]))
	for i := 0; i < count; i++ {
		entry := offset + 2 + i*12
		if entry+12 > len(tiff) {
			break
		}
		fields.decodeEntry(tiff, entry, order)
	}

	return fields
}

// decodeEntry decodes one 12-byte IFD entry into the field maps.
func (f ifdFields) decodeEntry(tiff []byte, entry int, order binary.ByteOrder) {
	tag := order.Uint16(tiff[entry : entry+2])
	kind := order.Uint16(tiff[entry+2 : entry+4])
	length := int(order.Uint32(tiff[entry+4 : entry+8]))
	value := int(order.Uint32(tiff[entry+8 : entry+12]))
	switch {
	case kind == exifTypeASCII:
		start := value
		if length <= 4 {
			start = entry + 8
		}
		if start >= 0 && start+length <= len(tiff) && length > 0 {
			f.text[tag] = string(bytes.TrimRight(tiff[start:start+length], "\x00"))
		}
	case kind == exifTypeRational && length >= 1:
		f.rationals[tag] = readRationals(tiff, value, length, order)
	default:
		f.offsets[tag] = value
	}
}

// readRationals reads up to three rational values from the value table.
func readRationals(tiff []byte, value, length int, order binary.ByteOrder) [3]float64 {
	var out [3]float64
	for j := 0; j < length && j < 3; j++ {
		at := value + j*8
		if at+8 > len(tiff) {
			break
		}
		numerator := float64(order.Uint32(tiff[at : at+4]))
		denominator := float64(order.Uint32(tiff[at+4 : at+8]))
		if denominator != 0 {
			out[j] = numerator / denominator
		}
	}

	return out
}

// gpsLine formats the GPS IFD's coordinates as a "GPS: lat, lon" line.
func gpsLine(tiff []byte, offset int, order binary.ByteOrder) string {
	gps := readIFD(tiff, offset, order)
	lat, latOK := gps.rationals[gpsTagLat]
	lon, lonOK := gps.rationals[gpsTagLon]
	if !latOK || !lonOK {
		return ""
	}
	latValue := lat[0] + lat[1]/60 + lat[2]/3600
	lonValue := lon[0] + lon[1]/60 + lon[2]/3600
	if gps.text[gpsTagLatRef] == "S" {
		latValue = -latValue
	}
	if gps.text[gpsTagLonRef] == "W" {
		lonValue = -lonValue
	}

	return fmt.Sprintf("GPS: %.6f, %.6f", latValue, lonValue)
}

func joinNonEmpty(parts ...string) string {
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			out = append(out, part)
		}
	}

	return joinSpace(out)
}

func joinSpace(parts []string) string {
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return parts[0]
	default:
		return parts[0] + " " + parts[1]
	}
}
