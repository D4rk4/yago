package pageindex

import (
	"fmt"
	"maps"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/crawlcapability"
	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

const (
	documentTypeText = 't'
	secondsPerDay    = 86400
)

func Build(page crawlcapability.ExtractedPage) (yacycrawlcontract.CrawledPageIndex, error) {
	urlHash, err := yacymodel.HashURL(page.CanonicalURL)
	if err != nil {
		return yacycrawlcontract.CrawledPageIndex{}, fmt.Errorf("hash url: %w", err)
	}

	order, occurrences, textStats := tokenize(page.Text)
	_, _, titleStats := tokenize(page.Title)
	dayNumber := dayNumberOf(page.FetchedAt)

	stats := documentWordStatistics{
		TextWordCount:  textStats.Words,
		TitleWordCount: titleStats.Words,
		PhraseCount:    textStats.Phrases,
	}
	shared := sharedProperties(page, urlHash.String(), stats, dayNumber)

	postings := make([]yacymodel.RWIPosting, 0, len(order))
	for _, word := range order {
		occurrence := occurrences[word]
		properties := map[string]string{}
		maps.Copy(properties, shared)
		properties[yacymodel.ColHitCount] = strconv.Itoa(occurrence.count)
		properties[yacymodel.ColTextPosition] = strconv.Itoa(occurrence.firstPosition)
		properties[yacymodel.ColPhraseRelativePos] = strconv.Itoa(occurrence.firstPositionInPhrase)
		properties[yacymodel.ColPhrasePosition] = strconv.Itoa(occurrence.firstPhraseNumber)
		postings = append(postings, yacymodel.RWIPosting{
			WordHash:   yacymodel.WordHash(word),
			Properties: properties,
		})
	}

	metadata := metadataRow(page, urlHash.String(), textStats.Words)

	return yacycrawlcontract.CrawledPageIndex{
		CanonicalURL: page.CanonicalURL,
		Postings:     postings,
		Metadata:     []yacymodel.URIMetadataRow{metadata},
	}, nil
}

// documentWordStatistics holds the shared RWI counters derived from tokenizing a page,
// grouped because they always travel together into sharedProperties.
type documentWordStatistics struct {
	TextWordCount  int
	TitleWordCount int
	PhraseCount    int
}

func sharedProperties(
	page crawlcapability.ExtractedPage,
	urlHash string,
	stats documentWordStatistics,
	dayNumber uint64,
) map[string]string {
	properties := map[string]string{
		yacymodel.ColURLHash:           urlHash,
		yacymodel.ColTextWordCount:     strconv.Itoa(stats.TextWordCount),
		yacymodel.ColTitleWordCount:    strconv.Itoa(stats.TitleWordCount),
		yacymodel.ColPhraseCount:       strconv.Itoa(stats.PhraseCount),
		yacymodel.ColDocType:           strconv.Itoa(documentTypeText),
		yacymodel.ColLocalLinkCount:    strconv.Itoa(page.LocalLinkCount),
		yacymodel.ColExternalLinkCount: strconv.Itoa(page.ExternalLinkCount),
		yacymodel.ColURLLength:         strconv.Itoa(len(page.CanonicalURL)),
		yacymodel.ColURLComponentCount: strconv.Itoa(componentCount(page.CanonicalURL)),
		yacymodel.ColLastModified:      strconv.FormatUint(dayNumber, 10),
		yacymodel.ColFreshUntil:        strconv.FormatUint(dayNumber, 10),
	}
	if page.Language != "" {
		properties[yacymodel.ColLanguage] = page.Language
	}
	return properties
}

func metadataRow(
	page crawlcapability.ExtractedPage,
	urlHash string,
	total int,
) yacymodel.URIMetadataRow {
	return yacymodel.URIMetadataRow{Properties: map[string]string{
		yacymodel.URLMetaHash:           urlHash,
		"dt":                            string(rune(documentTypeText)),
		"url":                           page.CanonicalURL,
		yacymodel.URLMetaColDescription: yacymodel.EncodeCompactWireForm(page.Title),
		"size":                          strconv.Itoa(len(page.Text)),
		"wc":                            strconv.Itoa(total),
		"llocal":                        strconv.Itoa(page.LocalLinkCount),
		"lother":                        strconv.Itoa(page.ExternalLinkCount),
	}}
}

func dayNumberOf(fetchedAt time.Time) uint64 {
	seconds := fetchedAt.Unix()
	if seconds < 0 {
		return 0
	}
	return uint64(seconds) / secondsPerDay
}

func componentCount(canonicalURL string) int {
	parsed, err := url.Parse(canonicalURL)
	if err != nil {
		return 0
	}
	trimmed := strings.Trim(parsed.Path, "/")
	if trimmed == "" {
		return 0
	}
	return strings.Count(trimmed, "/") + 1
}
