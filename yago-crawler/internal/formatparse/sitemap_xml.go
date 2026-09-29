package formatparse

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"strings"

	"github.com/D4rk4/yago/yago-crawler/internal/pageparse"
)

const sitemapProtocolNamespace = "http://www.sitemaps.org/schemas/sitemap/0.9"

func parseSitemapXML(rawURL string, body []byte) (pageparse.ParsedPage, bool, bool) {
	body = bytes.TrimPrefix(body, []byte("\xef\xbb\xbf"))
	decoder := xml.NewDecoder(bytes.NewReader(body))
	for {
		token, err := decoder.Token()
		if err != nil {
			return pageparse.ParsedPage{}, false, false
		}
		switch value := token.(type) {
		case xml.CharData:
			if strings.TrimSpace(string(value)) != "" {
				return pageparse.ParsedPage{}, false, false
			}
		case xml.StartElement:
			if value.Name.Space != sitemapProtocolNamespace ||
				value.Name.Local != "urlset" && value.Name.Local != "sitemapindex" {
				return pageparse.ParsedPage{}, false, false
			}
			page := pageparse.ParsedPage{URL: rawURL, Sitemap: true}
			if err := decoder.Skip(); err != nil {
				return page, false, true
			}

			return validateSitemapXMLTrailer(decoder, page)
		}
	}
}

func validateSitemapXMLTrailer(
	decoder *xml.Decoder,
	page pageparse.ParsedPage,
) (pageparse.ParsedPage, bool, bool) {
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return page, true, true
		}
		if err != nil {
			return page, false, true
		}
		switch value := token.(type) {
		case xml.CharData:
			if strings.TrimSpace(string(value)) != "" {
				return page, false, true
			}
		case xml.Comment:
			continue
		case xml.ProcInst:
			if strings.EqualFold(value.Target, "xml") {
				return page, false, true
			}
		default:
			return page, false, true
		}
	}
}
