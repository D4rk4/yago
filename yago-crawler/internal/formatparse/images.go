package formatparse

import (
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"fmt"
	"image"
	_ "image/gif"  // register GIF for DecodeConfig
	_ "image/jpeg" // register JPEG for DecodeConfig
	_ "image/png"  // register PNG for DecodeConfig
	"path"
	"strings"

	"github.com/D4rk4/yago/yago-crawler/internal/pageparse"
)

// parseImage indexes an image's metadata — dimensions, format, EXIF capture
// data for JPEG, title/description for SVG — never pixel content (no OCR,
// matching YaCy). The document carries itself as an image so the image
// vertical can surface standalone pictures.
func parseImage(rawURL, _ string, body []byte) (pageparse.ParsedPage, bool) {
	if urlExtension(rawURL) == "svg" || bytes.Contains(clipHead(body), []byte("<svg")) {
		return parseSVG(rawURL, body)
	}
	lines := imageMetadataLines(body)
	if len(lines) == 0 {
		return pageparse.ParsedPage{URL: rawURL}, false
	}
	title := imageTitle(rawURL)
	text := strings.Join(lines, "\n")

	return pageparse.ParsedPage{
		URL:   rawURL,
		Title: title,
		Text:  text,
		Images: []pageparse.ImageMetadata{
			{URL: rawURL, AltText: title},
		},
	}, true
}

// imageMetadataLines collects what the headers reveal about the image.
func imageMetadataLines(body []byte) []string {
	lines := make([]string, 0, 4)
	if config, format, err := image.DecodeConfig(bytes.NewReader(body)); err == nil {
		lines = append(
			lines,
			fmt.Sprintf("%s %dx%d", strings.ToUpper(format), config.Width, config.Height),
		)
	} else if line, ok := rawImageDimensions(body); ok {
		lines = append(lines, line)
	}
	lines = append(lines, jpegEXIFLines(body)...)

	return lines
}

// rawImageDimensions reads the width/height fields of the raster headers the
// stdlib decoders do not cover: BMP, PSD, and simple headerless WBMP.
func rawImageDimensions(body []byte) (string, bool) {
	switch {
	case len(body) >= 26 && body[0] == 'B' && body[1] == 'M':
		width := binary.LittleEndian.Uint32(body[18:22])
		height := binary.LittleEndian.Uint32(body[22:26])

		return fmt.Sprintf("BMP %dx%d", width, height), true
	case len(body) >= 26 && bytes.HasPrefix(body, []byte("8BPS")):
		height := binary.BigEndian.Uint32(body[14:18])
		width := binary.BigEndian.Uint32(body[18:22])

		return fmt.Sprintf("PSD %dx%d", width, height), true
	case len(body) >= 4 && body[0] == 0 && body[1] == 0:
		// WBMP type 0: two zero bytes then multi-byte width and height; the
		// common small-icon case fits single bytes.
		return fmt.Sprintf("WBMP %dx%d", body[2], body[3]), true
	default:
		return "", false
	}
}

// imageTitle names the image document after its file name.
func imageTitle(rawURL string) string {
	trimmed := rawURL
	if index := strings.IndexAny(trimmed, "?#"); index >= 0 {
		trimmed = trimmed[:index]
	}

	return path.Base(trimmed)
}

// svgDocument covers the SVG metadata the parser reads.
type svgDocument struct {
	Title  string `xml:"title"`
	Desc   string `xml:"desc"`
	Width  string `xml:"width,attr"`
	Height string `xml:"height,attr"`
}

// parseSVG indexes an SVG's title, description, and declared size.
func parseSVG(rawURL string, body []byte) (pageparse.ParsedPage, bool) {
	var parsed svgDocument
	if xml.Unmarshal(body, &parsed) != nil {
		return pageparse.ParsedPage{URL: rawURL}, false
	}
	lines := make([]string, 0, 3)
	if parsed.Title != "" {
		lines = append(lines, parsed.Title)
	}
	if parsed.Desc != "" {
		lines = append(lines, parsed.Desc)
	}
	if parsed.Width != "" && parsed.Height != "" {
		lines = append(lines, "SVG "+parsed.Width+"x"+parsed.Height)
	}
	if len(lines) == 0 {
		return pageparse.ParsedPage{URL: rawURL}, false
	}
	title := parsed.Title
	if title == "" {
		title = imageTitle(rawURL)
	}

	return pageparse.ParsedPage{
		URL:   rawURL,
		Title: title,
		Text:  strings.Join(lines, "\n"),
	}, true
}

func clipHead(body []byte) []byte {
	if len(body) > 512 {
		return body[:512]
	}

	return body
}
