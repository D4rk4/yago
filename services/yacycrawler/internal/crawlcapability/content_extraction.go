package crawlcapability

import "errors"

var (
	ErrUnsupportedMediaType = errors.New("unsupported media type")
	ErrUnextractable        = errors.New("unextractable")
)

type ContentExtraction interface {
	Extract(pageURL, contentType string, body []byte) ([]ExtractedContent, error)
}
