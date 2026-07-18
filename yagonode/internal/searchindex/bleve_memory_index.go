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

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/filetypeclass"
)

const (
	bleveBackendName = "bleve-memory"
	snippetRuneCap   = 320
)

type BleveMemoryIndex struct {
	mu            sync.RWMutex
	index         bleve.Index
	documents     map[string]documentstore.Document
	updatedAt     time.Time
	multilingual  bool
	analyzerScope bool
	now           func() time.Time
}

type bleveDocument struct {
	URL       string   `json:"url"`
	Title     string   `json:"title"`
	Headings  []string `json:"headings"`
	Body      string   `json:"body"`
	Anchors   []string `json:"anchors"`
	Language  string   `json:"language"`
	Host      string   `json:"host"`
	Candidate string   `json:"_candidate"`
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
		index:         index,
		documents:     map[string]documentstore.Document{},
		multilingual:  supportsMultilingualAnalyzers(index),
		analyzerScope: supportsAnalyzerScope(index),
		now:           time.Now,
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
	indexed, err := bleveDocumentFromStore(doc)
	if err != nil {
		return err
	}
	if err := b.index.Index(id, indexed); err != nil {
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

	searchRequest := bleve.NewSearchRequest(bleveSearchQuery(
		req,
		b.multilingual,
		b.analyzerScope,
	))
	scanAll := !req.CandidateOnly || req.WithFacets || hasPostFilters(req)
	searchRequest.Size = len(b.documents)
	if !scanAll {
		searchRequest.Size = diskSearchSize(req.MaxResults, len(b.documents))
	}
	searchRequest.Explain = req.Explain || req.IncludeFieldScores
	searchRequest.IncludeLocations = false
	searchRequest.Fields = []string{documentAnalyzerField}
	result, err := b.index.SearchInContext(ctx, searchRequest)
	if err != nil {
		return SearchResultSet{}, fmt.Errorf("search documents: %w", err)
	}

	results := make([]SearchResult, 0, min(req.MaxResults, len(result.Hits)))
	facets := newFacetCollector(req.WithFacets)
	total := 0
	for _, hit := range result.Hits {
		if !scanAll && len(results) >= req.MaxResults {
			break
		}
		doc, found := b.documents[hit.ID]
		if !found || !allowsDocument(doc, req) {
			continue
		}
		facets.observe(doc)
		total++
		if len(results) < req.MaxResults {
			mapped, err := searchResultFromStoredDocument(ctx, hit, doc, req)
			if err != nil {
				return SearchResultSet{}, err
			}
			results = append(results, mapped)
		}
	}
	if !scanAll {
		total = bleveDocumentCount(result.Total)
	}
	rescoreStoredQuotedPhrasePrefix(results, req)
	rescoreStoredProximity(results, req)

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

func bleveSearchQuery(
	req SearchRequest,
	multilingual bool,
	analyzerScope bool,
) blevequery.Query {
	weights := req.Weights.orDefault()
	// Interpret the query with the analyzers that serve its script (documents
	// were routed to a per-language analyzer at index time). Each candidate
	// contributes its own field matches; a document in language L matches only
	// the clauses analyzed the way its tokens were indexed. An index that
	// predates the per-language mappings can resolve none of these analyzers, so
	// there the query keeps the single default-analyzer clause set (empty name).
	analyzers := []string{""}
	if multilingual {
		analyzers = queryAnalyzers(queryAnalyzerText(req))
	}
	var main blevequery.Query
	switch {
	case req.Fuzzy:
		main = fuzzyRecoveryQuery(req, analyzers, weights, analyzerScope)
	case req.Relaxed || req.MinimumTermMatches > 0:
		main = minimumTermsQuery(req, analyzers, weights, analyzerScope)
	default:
		// The precise path requires every query word somewhere in the document,
		// matching YaCy's all-words RWI join; expansion terms only reorder.
		main = requiredTermsQuery(req, analyzers, weights, analyzerScope)
	}
	if len(req.ExcludeTerms) == 0 {
		return main
	}

	query := bleve.NewBooleanQuery()
	query.AddMust(main)
	for _, term := range req.ExcludeTerms {
		term = strings.TrimSpace(term)
		if term != "" {
			query.AddMustNot(crossFieldTermClause(term, analyzers, weights, 1))
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

func fieldMatch(field string, text string, boost float64) *blevequery.MatchQuery {
	query := bleve.NewMatchQuery(text)
	query.SetField(field)
	query.SetBoost(boost)
	query.SetOperator(blevequery.MatchQueryOperatorAnd)

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
		query.Analyzer = cjkQueryAnalyzer(analyzer)
	}

	return query
}

func bleveDocumentFromStore(doc documentstore.Document) (bleveDocument, error) {
	candidate, err := encodeStoredCandidateProjection(doc)
	if err != nil {
		return bleveDocument{}, fmt.Errorf("encode search candidate: %w", err)
	}

	return bleveDocument{
		URL:       documentURL(doc),
		Title:     doc.Title,
		Headings:  append([]string(nil), doc.Headings...),
		Body:      doc.ExtractedText,
		Anchors:   anchorTexts(doc.Inlinks),
		Language:  strings.ToLower(doc.Language),
		Host:      documentHost(doc),
		Analyzer:  detectDocumentAnalyzer(analyzerDetectionText(doc), doc.Language),
		Candidate: candidate,
	}, nil
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
	if req.IncludeRaw && !req.CandidateOnly {
		rawContent = doc.ExtractedText
	}
	published, dateConfidence := documentstore.PublicationDate(doc)
	proximity, orderedProximity := resultProximity(req, hit)
	analyzerName := indexedAnalyzerName(hit, doc)

	return SearchResult{
		DocumentID:           hit.ID,
		ClusterID:            doc.ClusterID,
		RepresentativeURL:    doc.RepresentativeURL,
		Title:                documentTitle(doc),
		URL:                  documentURL(doc),
		Snippet:              resultSnippet(hit, doc, req),
		RawContent:           rawContent,
		Score:                hit.Score,
		Explanation:          hitExplanation(req, hit),
		Quality:              doc.ContentQuality.Score,
		QualityKnown:         doc.ContentQuality.Known,
		SpamRisk:             doc.ContentQuality.SpamRisk,
		FunctionWordFraction: doc.ContentQuality.FunctionWordFraction,
		SymbolFraction:       doc.ContentQuality.SymbolFraction,
		AlphabeticFraction:   doc.ContentQuality.AlphabeticFraction,
		UniqueTokenFraction:  doc.ContentQuality.UniqueTokenFraction,
		Analyzer:             analyzerName,
		SafetyRating:         doc.ContentSafety.Rating,
		ExplicitProbability:  doc.ContentSafety.ExplicitProbability,
		SafetyConfidence:     doc.ContentSafety.Confidence,
		Proximity:            proximity,
		OrderedProximity:     orderedProximity,
		FieldScores:          hitFieldScores(req, hit),
		FieldTermPositions:   hitFieldTermPositions(req, hit, analyzerName),
		PublishedDate:        published,
		DateConfidence:       dateConfidence,
		Author:               doc.Metadata["author"],
		Keywords:             doc.Metadata["keywords"],
		Publisher:            doc.Metadata["publisher"],
		Language:             doc.Language,
		ContentType:          doc.ContentType,
		Size:                 len(doc.ExtractedText),
		Images:               resultImages(doc, req),
	}
}

func resultProximity(req SearchRequest, hit *search.DocumentMatch) (float64, float64) {
	if !req.IncludePositions {
		return 0, 0
	}
	terms := queryTermWords(req)

	return hitUnorderedProximity(hit, terms), hitOrderedProximity(hit, terms)
}

func hitExplanation(req SearchRequest, hit *search.DocumentMatch) string {
	if !req.Explain || hit.Expl == nil {
		return ""
	}

	return hit.Expl.String()
}

func allowsDocument(doc documentstore.Document, req SearchRequest) bool {
	host := documentHost(doc)
	if !allowsDocumentSource(doc, req, host) {
		return false
	}
	if !allowsDocumentPublication(doc, req) {
		return false
	}
	if !allowsDocumentAttributes(doc, req) {
		return false
	}

	return allowsDocumentLocation(doc, req, host)
}

func allowsDocumentSource(doc documentstore.Document, req SearchRequest, host string) bool {
	if req.SafeSearch && !allowsSafeDocument(doc, req.ContentDomain) {
		return false
	}
	if excludedDomain(host, req.ExcludeDomain) {
		return false
	}
	if !includedDomain(host, req.IncludeDomain) {
		return false
	}
	if req.Language != "" && !strings.EqualFold(doc.Language, req.Language) {
		return false
	}

	return true
}

func allowsDocumentPublication(doc documentstore.Document, req SearchRequest) bool {
	published := documentTime(doc)
	if !req.Since.IsZero() && published.Before(req.Since) {
		return false
	}
	if !req.Until.IsZero() && published.After(req.Until) {
		return false
	}

	return true
}

func allowsDocumentAttributes(doc documentstore.Document, req SearchRequest) bool {
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

	return true
}

func allowsDocumentLocation(doc documentstore.Document, req SearchRequest, host string) bool {
	if req.FileType != "" &&
		!filetypeclass.Matches(documentURL(doc), doc.ContentType, req.FileType) {
		return false
	}
	if req.InURL != "" &&
		!strings.Contains(strings.ToLower(documentURL(doc)), strings.ToLower(req.InURL)) {
		return false
	}
	if req.TLD != "" && !hostMatchesTLD(host, req.TLD) {
		return false
	}
	return allowsDocumentDate(doc, req)
}

func allowsSafeDocument(doc documentstore.Document, contentDomain string) bool {
	if doc.ContentSafety.Rating == documentstore.SafetyExplicit {
		return false
	}
	if strings.EqualFold(contentDomain, "image") &&
		doc.ContentSafety.Rating == documentstore.SafetyUnknown {
		return false
	}

	return true
}

// hostMatchesTLD reports whether host sits under the given top-level domain,
// matching the host itself or any subdomain of it.
func hostMatchesTLD(host, tld string) bool {
	host = strings.ToLower(host)
	tld = strings.TrimPrefix(strings.ToLower(tld), ".")

	return host == tld || strings.HasSuffix(host, "."+tld)
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
	published, _ := documentstore.PublicationDate(doc)

	return published
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
		if anchor.NoFollow || anchor.UserGenerated || anchor.Sponsored || anchor.Text == "" {
			continue
		}
		out = append(out, anchor.Text)
	}

	return out
}

func snippet(text string, fallback string) string {
	if strings.TrimSpace(text) == "" {
		return fallback
	}
	if textWithinRuneLimit(text, snippetRuneCap) {
		return normalizedSnippetWindow(text, false, fallback)
	}

	return snippetAtByte(text, 0, fallback)
}
