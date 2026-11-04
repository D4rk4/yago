package worker

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"time"

	"github.com/D4rk4/yago/yago-crawler2/internal/frontier"
	"github.com/D4rk4/yago/yago-crawler2/internal/httpfetch"
	"github.com/D4rk4/yago/yago-crawler2/internal/pageparse"
	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagomodel"
)

const maximumRWIPageWords = 8192

func buildIngestBatch(
	order yagocrawlcontract.CrawlOrder,
	page frontier.Page,
	response httpfetch.PageResponse,
	parsed pageparse.HTMLPage,
) yagocrawlcontract.IngestBatch {
	fetchedAt := response.FetchedAt.UTC()
	if fetchedAt.IsZero() {
		fetchedAt = time.Now().UTC()
	}
	canonicalURL := response.FinalURL
	if parsed.CanonicalURL != "" {
		canonicalURL = parsed.CanonicalURL
	}
	contentHash := sha256.Sum256(response.Body)
	text := strings.TrimSpace(parsed.Text)
	tokenized := yagomodel.TokenizeRWIText(
		strings.Join([]string{parsed.Title, strings.Join(parsed.Headings, " "), text}, " "),
		maximumRWIPageWords,
	)
	words := make([]yagomodel.RWIWord, 0, len(tokenized))
	positions := make(map[string]int, len(tokenized))
	for position, word := range tokenized {
		if existing, found := positions[word]; found {
			words[existing].Frequency++
			continue
		}
		positions[word] = len(words)
		words = append(words, yagomodel.RWIWord{
			Word:          word,
			Frequency:     1,
			FirstPosition: position,
		})
	}
	localLinks := 0
	remoteLinks := 0
	outlinks := make([]string, 0, len(parsed.Links))
	anchors := make([]yagocrawlcontract.OutboundAnchor, 0, len(parsed.Links))
	for _, link := range parsed.Links {
		outlinks = append(outlinks, link.URL)
		anchors = append(anchors, yagocrawlcontract.OutboundAnchor{
			TargetURL: link.URL,
			Text:      link.Text,
			NoFollow:  link.NoFollow,
		})
		if contentURLHost(link.URL) == contentURLHost(response.FinalURL) {
			localLinks++
		} else {
			remoteLinks++
		}
	}
	artifacts := yagomodel.BuildRWIPageArtifacts(yagomodel.RWIPage{
		URL:               response.FinalURL,
		Title:             parsed.Title,
		Language:          parsed.Language,
		WordCount:         len(tokenized),
		TitleWordCount:    len(yagomodel.TokenizeRWIText(parsed.Title, 255)),
		TextBytes:         len(text),
		LocalLinkCount:    localLinks,
		ExternalLinkCount: remoteLinks,
		Words:             words,
		LoadedAt:          fetchedAt,
		ModifiedAt:        fetchedAt,
		FreshUntil:        fetchedAt.Add(24 * time.Hour),
	}, len(words))
	return yagocrawlcontract.IngestBatch{
		SourceURL:     response.FinalURL,
		Provenance:    append([]byte(nil), order.Provenance...),
		ProfileHandle: order.Profile.Handle,
		ObservationID: page.ID,
		ObservedAt:    fetchedAt,
		Document: yagocrawlcontract.DocumentIngest{
			CanonicalURL:                canonicalURL,
			NormalizedURL:               response.FinalURL,
			Title:                       parsed.Title,
			Headings:                    append([]string(nil), parsed.Headings...),
			ExtractedText:               text,
			Language:                    parsed.Language,
			ContentType:                 response.ContentType,
			FetchStatus:                 strconv.Itoa(response.StatusCode),
			FetchedAt:                   fetchedAt,
			IndexedAt:                   time.Now().UTC(),
			FirstSeenAt:                 fetchedAt,
			ContentChangedAt:            fetchedAt,
			ContentHash:                 hex.EncodeToString(contentHash[:]),
			Outlinks:                    outlinks,
			OutboundAnchors:             anchors,
			OutboundAnchorEvidenceKnown: true,
		},
		Postings: artifacts.Postings,
		Metadata: []yagomodel.URIMetadataRow{artifacts.Metadata},
	}
}
