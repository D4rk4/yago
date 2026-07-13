package documentsearch

import "github.com/D4rk4/yago/yagonode/internal/documentstore"

func searchCorePublicationDate(doc documentstore.Document) (string, float64) {
	published, confidence := documentstore.PublicationDate(doc)
	published = published.UTC()
	if published.Year() <= 1 {
		return "", 0
	}

	return published.Format("20060102"), confidence
}
