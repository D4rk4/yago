package yagonode

import (
	"context"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/rwi"
	"github.com/D4rk4/yago/yagonode/internal/urlmeta"
)

const termSampleLimit = 20

// termSource adapts the RWI posting index and URL directory to the console's
// term-browser: it hashes a term, counts its postings, and resolves a bounded
// sample of postings to their document URLs.
type termSource struct {
	postings rwi.PostingIndex
	urls     urlmeta.URLDirectory
}

func newTermSource(postings rwi.PostingIndex, urls urlmeta.URLDirectory) adminui.TermSource {
	if postings == nil || urls == nil {
		return nil
	}

	return &termSource{postings: postings, urls: urls}
}

func (s *termSource) LookupTerm(ctx context.Context, term string) adminui.TermReport {
	report := adminui.TermReport{Term: term}
	if term == "" {
		return report
	}

	hash := yagomodel.WordHash(term)
	report.Hash = hash.String()
	count, err := s.postings.RWIURLCount(ctx, hash)
	if err != nil {
		report.Error = "The term lookup failed."

		return report
	}

	report.Count = count
	if count == 0 {
		report.NotFound = true

		return report
	}
	report.Sample = s.termSample(ctx, hash)

	return report
}

func (s *termSource) termSample(ctx context.Context, hash yagomodel.Hash) []adminui.TermPosting {
	hashes := make([]yagomodel.Hash, 0, termSampleLimit)
	scan := func(posting yagomodel.RWIPosting) (bool, error) {
		location, err := posting.URLHash()
		if err == nil {
			hashes = append(hashes, location.Hash())
		}

		return len(hashes) < termSampleLimit, nil
	}
	if err := s.postings.ScanWord(ctx, hash, scan); err != nil {
		return nil
	}

	rows, err := s.urls.RowsByHash(ctx, hashes)
	if err != nil {
		return nil
	}
	sample := make([]adminui.TermPosting, 0, len(rows))
	for _, row := range rows {
		rawURL, _ := yagomodel.DecodeWireForm(ctx, row.Properties[yagomodel.URLMetaURL])
		title, _ := row.Title(ctx)
		sample = append(sample, adminui.TermPosting{URL: rawURL, Title: title})
	}

	return sample
}

// indexSchemaGroups is the read-only reference of the fields each index stores,
// shown on the Index section.
func indexSchemaGroups() []adminui.SchemaGroup {
	return []adminui.SchemaGroup{
		{
			Title: "Full-text search index",
			Fields: []adminui.SchemaField{
				{Name: "url", Description: "Document URL, tokenized for matching."},
				{Name: "title", Description: "Document title."},
				{Name: "headings", Description: "Heading text (h1–h6)."},
				{Name: "anchors", Description: "Anchor / link text pointing at the document."},
				{Name: "body", Description: "Main extracted body text."},
			},
		},
		{
			Title: "Reverse word index (RWI posting)",
			Fields: []adminui.SchemaField{
				{Name: "word hash", Description: "Hash of the indexed term (the posting key)."},
				{Name: "url hash", Description: "Hash of the document carrying the term."},
				{Name: "hit count", Description: "Occurrences of the term in the document."},
				{
					Name:        "word / phrase position",
					Description: "Position of the term within the text.",
				},
				{Name: "flags", Description: "Per-posting classification flags."},
			},
		},
		{
			Title: "URL metadata",
			Fields: []adminui.SchemaField{
				{Name: "url", Description: "Canonical document URL."},
				{Name: "description", Description: "Title / description text."},
				{Name: "hash", Description: "Stable URL hash used as the row key."},
				{Name: "size", Description: "Document size in bytes."},
				{Name: "word count", Description: "Number of words in the document."},
			},
		},
	}
}
