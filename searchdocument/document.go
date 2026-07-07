package searchdocument

import "time"

type Document struct {
	Title     string    `json:"title"`
	URL       string    `json:"url"`
	Content   string    `json:"content"`
	CrawledAt time.Time `json:"crawled_at"`
	Language  string    `json:"language"`
}
