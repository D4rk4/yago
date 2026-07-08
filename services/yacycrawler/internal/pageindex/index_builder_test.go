package pageindex_test

import (
	"testing"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/crawlcapability"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/pageindex"
	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

func samplePage() crawlcapability.ExtractedPage {
	return crawlcapability.ExtractedPage{
		CanonicalURL:      "http://example.com/article",
		Title:             "Hello World",
		Text:              "the quick brown fox the fox",
		Language:          "en",
		FetchedAt:         time.Unix(1_700_000_000, 0),
		LocalLinkCount:    3,
		ExternalLinkCount: 1,
	}
}

func TestBuildProducesParseablePostings(t *testing.T) {
	index, err := pageindex.Build(samplePage())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if index.CanonicalURL != "http://example.com/article" {
		t.Fatalf("canonical url: %q", index.CanonicalURL)
	}
	if len(index.Postings) == 0 {
		t.Fatal("no postings")
	}
	for _, posting := range index.Postings {
		if _, err := yacymodel.ParseRWIPosting(posting.String()); err != nil {
			t.Fatalf("posting %q not parseable: %v", posting.String(), err)
		}
	}
}

func TestBuildCountsRepeatedWords(t *testing.T) {
	index, err := pageindex.Build(samplePage())
	if err != nil {
		t.Fatal(err)
	}
	foxHash := yacymodel.WordHash("fox")
	var found bool
	for _, posting := range index.Postings {
		if posting.WordHash == foxHash {
			found = true
			if posting.Properties[yacymodel.ColHitCount] != "2" {
				t.Fatalf("fox hit count = %q, want 2", posting.Properties[yacymodel.ColHitCount])
			}
		}
	}
	if !found {
		t.Fatal("word 'fox' not in postings")
	}
}

func TestBuildMetadataParseableAndCarriesURLHash(t *testing.T) {
	index, err := pageindex.Build(samplePage())
	if err != nil {
		t.Fatal(err)
	}
	if len(index.Metadata) != 1 {
		t.Fatalf("want one metadata row, got %d", len(index.Metadata))
	}
	row := index.Metadata[0]
	if _, err := yacymodel.ParseURIMetadataRow(row.String()); err != nil {
		t.Fatalf("metadata row not parseable: %v", err)
	}
	urlHash, err := yacymodel.HashURL("http://example.com/article")
	if err != nil {
		t.Fatal(err)
	}
	if row.Properties[yacymodel.URLMetaHash] != urlHash.String() {
		t.Fatalf("metadata url hash = %q, want %q",
			row.Properties[yacymodel.URLMetaHash], urlHash.String())
	}
}

func TestBuildMetadataSurvivesCommaInTitleAndURL(t *testing.T) {
	page := samplePage()
	page.CanonicalURL = "http://example.com/article?ids=1,2,3"
	page.Title = "Fourth of July fireworks, 1986 - Example"
	index, err := pageindex.Build(page)
	if err != nil {
		t.Fatal(err)
	}
	row := index.Metadata[0]
	parsed, err := yacymodel.ParseURIMetadataRow(row.String())
	if err != nil {
		t.Fatalf("metadata row not parseable: %v", err)
	}
	title, err := parsed.Title(t.Context())
	if err != nil {
		t.Fatalf("Title: %v", err)
	}
	if title != page.Title {
		t.Fatalf("title = %q, want %q", title, page.Title)
	}
}

func TestBuildOmitsLanguageWhenAbsent(t *testing.T) {
	page := samplePage()
	page.Language = ""
	index, err := pageindex.Build(page)
	if err != nil {
		t.Fatal(err)
	}
	for _, posting := range index.Postings {
		if _, ok := posting.Properties[yacymodel.ColLanguage]; ok {
			t.Fatal("language column should be omitted when unknown")
		}
	}
}

func TestBuildDropsWordsShorterThanTwoCharacters(t *testing.T) {
	page := samplePage()
	page.Text = "a fox I saw"
	index, err := pageindex.Build(page)
	if err != nil {
		t.Fatal(err)
	}
	for _, posting := range index.Postings {
		if posting.WordHash == yacymodel.WordHash("a") ||
			posting.WordHash == yacymodel.WordHash("i") {
			t.Fatalf("short word should not be indexed: %q", posting.String())
		}
	}
	if len(index.Postings) != 2 {
		t.Fatalf("want postings for 'fox' and 'saw' only, got %d", len(index.Postings))
	}
}

func TestBuildKeepsHyphenatedCompoundAsOneWord(t *testing.T) {
	page := samplePage()
	page.Text = "state-of-the-art design"
	index, err := pageindex.Build(page)
	if err != nil {
		t.Fatal(err)
	}
	compoundHash := yacymodel.WordHash("state-of-the-art")
	var found bool
	for _, posting := range index.Postings {
		if posting.WordHash == compoundHash {
			found = true
		}
	}
	if !found {
		t.Fatal("hyphenated compound should be indexed as a single word")
	}
}

func TestBuildKeepsDigitSeparatedNumberAsOneWord(t *testing.T) {
	page := samplePage()
	page.Text = "the price is 1,234.56 today"
	index, err := pageindex.Build(page)
	if err != nil {
		t.Fatal(err)
	}
	numberHash := yacymodel.WordHash("1,234.56")
	var found bool
	for _, posting := range index.Postings {
		if posting.WordHash == numberHash {
			found = true
		}
	}
	if !found {
		t.Fatal("number with digit separators should be indexed as a single token")
	}
}

func TestBuildCountsPhrasesAndPhrasePositions(t *testing.T) {
	page := samplePage()
	page.Text = "the quick fox jumps. the lazy dog sleeps."
	index, err := pageindex.Build(page)
	if err != nil {
		t.Fatal(err)
	}
	for _, posting := range index.Postings {
		if posting.Properties[yacymodel.ColPhraseCount] != "2" {
			t.Fatalf("phrase count = %q, want 2", posting.Properties[yacymodel.ColPhraseCount])
		}
	}
	jumpsHash := yacymodel.WordHash("jumps")
	sleepsHash := yacymodel.WordHash("sleeps")
	var jumpsPhrase, sleepsPhrase string
	for _, posting := range index.Postings {
		if posting.WordHash == jumpsHash {
			jumpsPhrase = posting.Properties[yacymodel.ColPhrasePosition]
		}
		if posting.WordHash == sleepsHash {
			sleepsPhrase = posting.Properties[yacymodel.ColPhrasePosition]
		}
	}
	if jumpsPhrase == "" || jumpsPhrase == sleepsPhrase {
		t.Fatalf(
			"words in different sentences should get different phrase numbers, got %q and %q",
			jumpsPhrase,
			sleepsPhrase,
		)
	}
}
