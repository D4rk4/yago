package adminui

import "context"

// TermPosting is one posting for a looked-up term, resolved to its document.
type TermPosting struct {
	URL   string
	Title string
}

// TermReport is the result of an admin term lookup against the RWI index.
type TermReport struct {
	Term        string
	Hash        string
	Count       int
	Sample      []TermPosting
	NotFound    bool
	Error       error
	SampleError error
}

// TermSource looks up a term's posting count and a bounded sample of its
// postings for admin debugging. A nil source hides the term browser.
type TermSource interface {
	LookupTerm(ctx context.Context, term string) TermReport
}

// SchemaField is one field in an index-schema group.
type SchemaField struct {
	Name        string
	Description string
}

// SchemaGroup is a named set of index-schema fields shown as read-only reference.
type SchemaGroup struct {
	Title  string
	Fields []SchemaField
}
