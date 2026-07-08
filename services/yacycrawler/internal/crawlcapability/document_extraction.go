package crawlcapability

type DocumentExtraction interface {
	Extract(resourceURL, contentType string, body []byte) ([]ExtractedDocument, error)
}
