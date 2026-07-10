package searchindex

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/index/scorch"
	"github.com/blevesearch/bleve/v2/mapping"
	"github.com/blevesearch/bleve/v2/search"
	blevequery "github.com/blevesearch/bleve/v2/search/query"

	"github.com/D4rk4/yago/yagonode/internal/contentprior"
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

const (
	bleveBackendName = "bleve-memory"
	snippetRuneCap   = 320
)

type BleveMemoryIndex struct {
	mu           sync.RWMutex
	index        bleve.Index
	documents    map[string]documentstore.Document
	updatedAt    time.Time
	gram         bool
	multilingual bool
	now          func() time.Time
}

type bleveDocument struct {
	URL      string   `json:"url"`
	Title    string   `json:"title"`
	Headings []string `json:"headings"`
	Body     string   `json:"body"`
	Anchors  []string `json:"anchors"`
	Language string   `json:"language"`
	Host     string   `json:"host"`
	// Analyzer is the TypeField that routes the document to its per-language
	// analyzer mapping; see documentAnalyzerField.
	Analyzer string `json:"_analyzer"`
}

// BleveType routes the document to its per-language analyzer mapping. bleve
// reads the type of a struct document from this interface method (the TypeField
// property path only resolves for map documents), so without it every document
// would fall to the default no-stemming mapping.
func (d bleveDocument) BleveType() string {
	return d.Analyzer
}

// newBleveMemory opens the in-memory fallback index on the scorch backend
// (bleve.NewMemOnly's upside-down backend ignores the BM25 scoring model and
// mishandles positional queries). An empty path keeps scorch entirely in
// memory, so the fallback ranks and matches identically to the on-disk shards.
var newBleveMemory = func(indexMapping mapping.IndexMapping) (bleve.Index, error) {
	return bleve.NewUsing("", indexMapping, scorch.Name, scorch.Name, nil)
}

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
		index:        index,
		documents:    map[string]documentstore.Document{},
		gram:         supportsGramAnalyzer(index),
		multilingual: supportsMultilingualAnalyzers(index),
		now:          time.Now,
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

	searchRequest := bleve.NewSearchRequest(bleveSearchQuery(req, b.gram, b.multilingual))
	searchRequest.Size = len(b.documents)
	searchRequest.Explain = req.Explain
	searchRequest.IncludeLocations = req.IncludePositions
	result, err := b.index.SearchInContext(ctx, searchRequest)
	if err != nil {
		return SearchResultSet{}, fmt.Errorf("search documents: %w", err)
	}

	results := make([]SearchResult, 0, min(req.MaxResults, len(result.Hits)))
	facets := newFacetCollector(req.WithFacets)
	total := 0
	for _, hit := range result.Hits {
		doc, found := b.documents[hit.ID]
		if !found || !allowsDocument(doc, req) {
			continue
		}
		facets.observe(doc)
		total++
		if len(results) < req.MaxResults {
			results = append(
				results,
				searchResultFromDocument(hit, doc, req),
			)
		}
	}

	return SearchResultSet{Results: results, Total: total, Facets: facets.groups()}, nil
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

func bleveSearchQuery(req SearchRequest, gram bool, multilingual bool) blevequery.Query {
	weights := req.Weights.orDefault()
	// Interpret the query with the analyzers that serve its script (documents
	// were routed to a per-language analyzer at index time). Each candidate
	// contributes its own field matches; a document in language L matches only
	// the clauses analyzed the way its tokens were indexed. An index that
	// predates the per-language mappings can resolve none of these analyzers, so
	// there the query keeps the single default-analyzer clause set (empty name).
	analyzers := []string{""}
	if multilingual {
		analyzers = queryAnalyzers(req.Query)
	}
	primary := analyzers[0]
	var main blevequery.Query
	if req.Fuzzy {
		main = fuzzyRecoveryQuery(req, gram, analyzers, weights)
	} else {
		// The precise path requires every query word somewhere in the document,
		// matching YaCy's all-words RWI join; expansion terms only reorder.
		main = requiredTermsQuery(req, analyzers, weights)
	}
	phrases := phraseBoosts(req.Phrases, weights, primary)
	// Term-dependency boosts (SDM, Metzler & Croft 2005): documents where
	// adjacent query words appear as an ordered pair outrank bags of the same
	// words. The fuzzy recovery path skips them — its terms are approximate.
	var bigrams []blevequery.Query
	if !req.Fuzzy {
		bigrams = sdmBigramBoosts(req.Terms, weights, primary)
	}
	if len(req.ExcludeTerms) == 0 && len(phrases) == 0 && len(bigrams) == 0 {
		return main
	}

	query := bleve.NewBooleanQuery()
	query.AddMust(main)
	for _, phrase := range phrases {
		query.AddShould(phrase)
	}
	for _, bigram := range bigrams {
		query.AddShould(bigram)
	}
	for _, term := range req.ExcludeTerms {
		term = strings.TrimSpace(term)
		if term != "" {
			query.AddMustNot(fieldMatchWithAnalyzer("body", term, 1, primary))
			query.AddMustNot(fieldMatchWithAnalyzer("title", term, 1, primary))
		}
	}

	return query
}

// textSearchFields are the fields analyzed with a per-language stemmer (the url
// field keeps its own punctuation splitter).
func textSearchFields() []string {
	return []string{"title", "headings", "anchors", "body"}
}

// textFieldWeight is the ranking weight for one stemmed text field.
func textFieldWeight(field string, weights RankingWeights) float64 {
	switch field {
	case "title":
		return weights.Title
	case "headings":
		return weights.Headings
	case "anchors":
		return weights.Anchors
	default:
		return weights.Body
	}
}

// phraseBoosts turns each quoted phrase into an optional, weighted phrase match
// across the text fields, analyzed with the query's primary analyzer. Added as
// SHOULD clauses, they lift documents where the words appear adjacently without
// excluding the term-only matches the main disjunction already covers.
func phraseBoosts(phrases []string, weights RankingWeights, analyzer string) []blevequery.Query {
	boosts := make([]blevequery.Query, 0, len(phrases))
	for _, phrase := range phrases {
		phrase = strings.TrimSpace(phrase)
		if phrase == "" {
			continue
		}
		boosts = append(boosts, bleve.NewDisjunctionQuery(
			fieldPhrase("title", phrase, weights.Title, analyzer),
			fieldPhrase("headings", phrase, weights.Headings, analyzer),
			fieldPhrase("body", phrase, weights.Body, analyzer),
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

// fieldMatchWithAnalyzer is a field match whose query text is analyzed with an
// explicit analyzer, so the query is stemmed the same way the target
// document's language was at index time. An empty analyzer leaves the field's
// own mapping analyzer in effect (the legacy pre-multilingual behavior).
func fieldMatchWithAnalyzer(
	field string,
	text string,
	boost float64,
	analyzer string,
) *blevequery.MatchQuery {
	query := fieldMatch(field, text, boost)
	if analyzer != "" {
		query.Analyzer = analyzer
	}

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

func fieldPhrase(
	field string,
	text string,
	boost float64,
	analyzer string,
) *blevequery.MatchPhraseQuery {
	query := bleve.NewMatchPhraseQuery(text)
	query.SetField(field)
	query.SetBoost(boost)
	if analyzer != "" {
		query.Analyzer = analyzer
	}

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
		Analyzer: detectDocumentAnalyzer(analyzerDetectionText(doc), doc.Language),
	}
}

// analyzerDetectionText assembles the text language detection runs on: the
// title and extracted body carry the most representative language signal.
func analyzerDetectionText(doc documentstore.Document) string {
	return strings.TrimSpace(doc.Title + " " + doc.ExtractedText)
}

func searchResultFromDocument(
	hit *search.DocumentMatch,
	doc documentstore.Document,
	req SearchRequest,
) SearchResult {
	rawContent := ""
	if req.IncludeRaw {
		rawContent = doc.ExtractedText
	}

	return SearchResult{
		DocumentID:         hit.ID,
		Title:              documentTitle(doc),
		URL:                documentURL(doc),
		Snippet:            queryBiasedSnippet(doc.ExtractedText, req.Terms, documentTitle(doc)),
		RawContent:         rawContent,
		Score:              hit.Score,
		Explanation:        hitExplanation(req, hit),
		Quality:            contentprior.Score(doc.ExtractedText),
		Proximity:          unorderedProximity(doc.ExtractedText, req.Terms),
		FieldScores:        hitFieldScores(req, hit),
		FieldTermPositions: hitFieldTermPositions(req, hit),
		PublishedDate:      documentTime(doc),
		Author:             doc.Metadata["author"],
		Keywords:           doc.Metadata["keywords"],
		Publisher:          doc.Metadata["publisher"],
		ContentType:        doc.ContentType,
		Size:               len(doc.ExtractedText),
		Images:             resultImages(doc, req),
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
	if req.Author != "" &&
		!strings.Contains(
			strings.ToLower(doc.Metadata["author"]),
			strings.ToLower(req.Author),
		) {
		return false
	}
	if req.Near && !termsNear(doc.ExtractedText, req.Terms) {
		return false
	}
	if !allowsContentDomain(doc, req.ContentDomain) {
		return false
	}
	if !allowsDocumentDate(doc, req) {
		return false
	}

	return true
}

// allowsDocumentDate applies the optional document-date bounds; when a bound
// is set, undated documents do not qualify.
func allowsDocumentDate(doc documentstore.Document, req SearchRequest) bool {
	if req.MinDate.IsZero() && req.MaxDate.IsZero() {
		return true
	}
	when := documentTime(doc)
	if when.IsZero() {
		return false
	}
	if !req.MinDate.IsZero() && when.Before(req.MinDate) {
		return false
	}
	if !req.MaxDate.IsZero() && when.After(req.MaxDate) {
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

// resultImages surfaces the document's extracted images for the image vertical;
// other domains carry none to keep result payloads lean.
func resultImages(doc documentstore.Document, req SearchRequest) []ResultImage {
	if !strings.EqualFold(req.ContentDomain, "image") {
		return nil
	}
	images := make([]ResultImage, 0, len(doc.Images))
	for _, image := range doc.Images {
		if image.URL == "" {
			continue
		}
		images = append(images, ResultImage{URL: image.URL, Alt: image.AltText})
	}

	return images
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
