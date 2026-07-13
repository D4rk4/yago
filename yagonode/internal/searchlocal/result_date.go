package searchlocal

import "time"

func compactPublicationDate(published time.Time) string {
	published = published.UTC()
	if published.Year() <= 1 {
		return ""
	}

	return published.Format("20060102")
}
