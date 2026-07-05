package searchindex

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/search"
	blevequery "github.com/blevesearch/bleve/v2/search/query"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

const (
	bleveBackendName = "bleve-memory"
	snippetRuneCap   = 320
)

type BleveMemoryIndex struct {
	mu        sync.RWMutex
	index     bleve.Index
	documents map[string]documentstore.Document
	updatedAt time.Time
	now       func() time.Time
}

type bleveDocument struct {
	URL      string   `json:"url"`
	Title    string   `json:"title"`
	Headings []string `json:"headings"`
	Body     string   `json:"body"`
	Anchors  []string `json:"anchors"`
	Language string   `json:"language"`
	Host     string   `json:"host"`
}

var newBleveMemory = bleve.NewMemOnly

func NewBleveMemoryIndex(
	ctx context.Context,
	stored documentstore.StoredDocuments,
) (*BleveMemoryIndex, error) {
	indexMapping, err := newSearchIndexMapping()
	if err != nil {
		return nil, fmt.Errorf("build search index mapping: %w", err)
	}
	index, err := newBleveMemory(indexMapping)
	if err != nil {
		return nil, fmt.Errorf("open bleve memory index: %w", err)
	}

	out := &BleveMemoryIndex{
		index:     index,
		documents: map[string]documentstore.Document{},
		now:       time.Now,
	}
	if stored == nil {
		return out, nil
	}
	if err := stored.StoredDocuments(ctx, func(doc documentstore.Document) (bool, error) {
		if err := out.Index(ctx, doc); err != nil {
			return false, err
		}

		return true, nil
	}); err != nil {
		_ = index.Close()
		return nil, fmt.Errorf("rebuild bleve memory index: %w", err)
	}

	return out, nil
}

func (b *BleveMemoryIndex) Index(
	ctx context.Context,
	doc documentstore.Document,
) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("context: %w", err)
	}
	id := documentID(doc)
	if id == "" {
		return fmt.Errorf("document id required")
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.index.Index(id, bleveDocumentFromStore(doc)); err != nil {
		return fmt.Errorf("index document: %w", err)
	}
	b.documents[id] = doc
	b.updatedAt = b.now()

	return nil
}

func (b *BleveMemoryIndex) Delete(ctx context.Context, docID string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("context: %w", err)
	}
	docID = strings.TrimSpace(docID)
	if docID == "" {
		return fmt.Errorf("document id required")
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.index.Delete(docID); err != nil {
		return fmt.Errorf("delete document: %w", err)
	}
	delete(b.documents, docID)
	b.updatedAt = b.now()

	return nil
}

func (b *BleveMemoryIndex) Search(
	ctx context.Context,
	req SearchRequest,
) (SearchResultSet, error) {
	req.Query = strings.TrimSpace(req.Query)
	if req.Query == "" || req.MaxResults <= 0 {
		return SearchResultSet{}, nil
	}

	b.mu.RLock()
	defer b.mu.RUnlock()
	if len(b.documents) == 0 {
		return SearchResultSet{}, nil
	}

	searchRequest := bleve.NewSearchRequest(bleveSearchQuery(req))
	searchRequest.Size = len(b.documents)
	searchRequest.Explain = req.Explain
	result, err := b.index.SearchInContext(ctx, searchRequest)
	if err != nil {
		return SearchResultSet{}, fmt.Errorf("search documents: %w", err)
	}

	results := make([]SearchResult, 0, min(req.MaxResults, len(result.Hits)))
	total := 0
	for _, hit := range result.Hits {
		doc, found := b.documents[hit.ID]
		if !found || !allowsDocument(doc, req) {
			continue
		}
		total++
		if len(results) < req.MaxResults {
			results = append(
				results,
				searchResultFromDocument(hit.ID, doc, req, hit.Score, hitExplanation(req, hit)),
			)
		}
	}

	return SearchResultSet{Results: results, Total: total}, nil
}

func (b *BleveMemoryIndex) Stats(context.Context) (IndexStats, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return IndexStats{
		Documents: len(b.documents),
		Backend:   bleveBackendName,
		UpdatedAt: b.updatedAt,
	}, nil
}

func bleveSearchQuery(req SearchRequest) blevequery.Query {
	weights := req.Weights.orDefault()
	main := bleve.NewDisjunctionQuery(
		fieldMatch("title", req.Query, weights.Title),
		fieldMatch("headings", req.Query, weights.Headings),
		fieldMatch("anchors", req.Query, weights.Anchors),
		fieldMatch("body", req.Query, weights.Body),
		fieldMatch("url", req.Query, weights.URL),
		// Language-agnostic trigram recall: a document whose text contains every
		// trigram of a query word matches even when the word is a morphological
		// variant or truncation (e.g. "зеленски" -> "зеленский"), for any script.
		gramMatch("title"+gramFieldSuffix, req.Query, weights.Title*gramWeightFactor),
		gramMatch("headings"+gramFieldSuffix, req.Query, weights.Headings*gramWeightFactor),
		gramMatch("anchors"+gramFieldSuffix, req.Query, weights.Anchors*gramWeightFactor),
		gramMatch("body"+gramFieldSuffix, req.Query, weights.Body*gramWeightFactor),
	)
	phrases := phraseBoosts(req.Phrases, weights)
	if len(req.ExcludeTerms) == 0 && len(phrases) == 0 {
		return main
	}

	query := bleve.NewBooleanQuery()
	query.AddMust(main)
	for _, phrase := range phrases {
		query.AddShould(phrase)
	}
	for _, term := range req.ExcludeTerms {
		term = strings.TrimSpace(term)
		if term != "" {
			query.AddMustNot(fieldMatch("body", term, 1))
			query.AddMustNot(fieldMatch("title", term, 1))
		}
	}

	return query
}

// phraseBoosts turns each quoted phrase into an optional, weighted phrase match
// across the text fields. Added as SHOULD clauses, they lift documents where the
// words appear adjacently without excluding the term-only matches the main
// disjunction already covers.
func phraseBoosts(phrases []string, weights RankingWeights) []blevequery.Query {
	boosts := make([]blevequery.Query, 0, len(phrases))
	for _, phrase := range phrases {
		phrase = strings.TrimSpace(phrase)
		if phrase == "" {
			continue
		}
		boosts = append(boosts, bleve.NewDisjunctionQuery(
			fieldPhrase("title", phrase, weights.Title),
			fieldPhrase("headings", phrase, weights.Headings),
			fieldPhrase("body", phrase, weights.Body),
		))
	}

	return boosts
}

func fieldMatch(field string, text string, boost float64) *blevequery.MatchQuery {
	query := bleve.NewMatchQuery(text)
	query.SetField(field)
	query.SetBoost(boost)

	return query
}

// gramMatch matches a trigram sub-field with AND semantics: every trigram the
// query analyzes into must be present in the document, so the clause fires only
// for words that actually share the query's character runs (precision, and no
// flooding on common single grams) rather than any partial overlap.
func gramMatch(field string, text string, boost float64) *blevequery.MatchQuery {
	query := bleve.NewMatchQuery(text)
	query.SetField(field)
	query.SetBoost(boost)
	query.SetOperator(blevequery.MatchQueryOperatorAnd)
	// The gram sub-field is named "<field>_gram" but lives at path "<field>", so
	// the mapping cannot resolve its analyzer by name; set it explicitly so the
	// query text is cut into the same trigrams the field was indexed with.
	query.Analyzer = searchGramAnalyzer

	return query
}

func fieldPhrase(field string, text string, boost float64) *blevequery.MatchPhraseQuery {
	query := bleve.NewMatchPhraseQuery(text)
	query.SetField(field)
	query.SetBoost(boost)

	return query
}

func bleveDocumentFromStore(doc documentstore.Document) bleveDocument {
	return bleveDocument{
		URL:      documentURL(doc),
		Title:    doc.Title,
		Headings: append([]string(nil), doc.Headings...),
		Body:     doc.ExtractedText,
		Anchors:  anchorTexts(doc.Inlinks),
		Language: strings.ToLower(doc.Language),
		Host:     documentHost(doc),
	}
}

func searchResultFromDocument(
	documentID string,
	doc documentstore.Document,
	req SearchRequest,
	score float64,
	explanation string,
) SearchResult {
	rawContent := ""
	if req.IncludeRaw {
		rawContent = doc.ExtractedText
	}

	return SearchResult{
		DocumentID:    documentID,
		Title:         documentTitle(doc),
		URL:           documentURL(doc),
		Snippet:       snippet(doc.ExtractedText, documentTitle(doc)),
		RawContent:    rawContent,
		Score:         score,
		Explanation:   explanation,
		PublishedDate: documentTime(doc),
	}
}

func hitExplanation(req SearchRequest, hit *search.DocumentMatch) string {
	if !req.Explain || hit.Expl == nil {
		return ""
	}

	return hit.Expl.String()
}

func allowsDocument(doc documentstore.Document, req SearchRequest) bool {
	host := documentHost(doc)
	if excludedDomain(host, req.ExcludeDomain) {
		return false
	}
	if !includedDomain(host, req.IncludeDomain) {
		return false
	}
	if req.Language != "" && !strings.EqualFold(doc.Language, req.Language) {
		return false
	}
	published := documentTime(doc)
	if !req.Since.IsZero() && published.Before(req.Since) {
		return false
	}
	if !req.Until.IsZero() && published.After(req.Until) {
		return false
	}

	return true
}

func includedDomain(host string, domains []string) bool {
	if len(domains) == 0 {
		return true
	}
	for _, domain := range domains {
		if domainMatches(host, domain) {
			return true
		}
	}

	return false
}

func excludedDomain(host string, domains []string) bool {
	for _, domain := range domains {
		if domainMatches(host, domain) {
			return true
		}
	}

	return false
}

func domainMatches(host string, domain string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	domain = strings.ToLower(strings.Trim(strings.TrimSpace(domain), "."))
	if host == "" || domain == "" {
		return false
	}

	return host == domain || strings.HasSuffix(host, "."+domain)
}

func documentID(doc documentstore.Document) string {
	if doc.NormalizedURL != "" {
		return doc.NormalizedURL
	}

	return doc.CanonicalURL
}

func documentURL(doc documentstore.Document) string {
	if doc.NormalizedURL != "" {
		return doc.NormalizedURL
	}

	return doc.CanonicalURL
}

func documentTitle(doc documentstore.Document) string {
	if doc.Title != "" {
		return doc.Title
	}

	return documentURL(doc)
}

func documentHost(doc documentstore.Document) string {
	parsed, err := url.Parse(documentURL(doc))
	if err != nil {
		return ""
	}

	return strings.ToLower(parsed.Hostname())
}

func documentTime(doc documentstore.Document) time.Time {
	if !doc.FetchedAt.IsZero() {
		return doc.FetchedAt
	}

	return doc.IndexedAt
}

func anchorTexts(anchors []documentstore.AnchorText) []string {
	out := make([]string, 0, len(anchors))
	for _, anchor := range anchors {
		out = append(out, anchor.Text)
	}

	return out
}

func snippet(text string, fallback string) string {
	text = strings.Join(strings.Fields(text), " ")
	if text == "" {
		return fallback
	}
	runes := []rune(text)
	if len(runes) <= snippetRuneCap {
		return text
	}

	return string(runes[:snippetRuneCap])
}
