//go:build e2e

package e2e

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

const rankingPromotionDocumentWords = 60

var rankingPromotionClusters = []string{
	"query 000",
	"query 001",
	"query 007",
	"query 009",
	"query 010",
	"query 015",
	"query 016",
	"query 033",
	"query 035",
	"query 040",
	"query 041",
	"query 047",
	"query 050",
	"query 055",
	"query 077",
	"query 083",
	"query 084",
	"query 086",
	"query 088",
	"query 097",
	"query 100",
	"query 109",
}

var rankingPromotionStopwords = []string{
	"the", "and", "of", "in", "to", "for", "with", "on", "at", "from",
}

type rankingPromotionCorpus struct {
	queries   []rankingPromotionQuery
	documents []rankingPromotionDocument
}

type rankingPromotionQuery struct {
	query     string
	cluster   string
	badURL    string
	middleURL string
	goodURL   string
}

type rankingPromotionDocument struct {
	url      string
	document yagocrawlcontract.DocumentIngest
}

type rankingPromotionVariant struct {
	name             string
	grade            int
	stopwordInterval int
	titleMatch       bool
	headingMatch     bool
}

func newRankingPromotionCorpus() rankingPromotionCorpus {
	variants := []rankingPromotionVariant{
		{name: "bad", grade: 0, stopwordInterval: 30, titleMatch: true, headingMatch: true},
		{name: "middle", grade: 1, stopwordInterval: 10, headingMatch: true},
		{name: "good", grade: 3, stopwordInterval: 4},
	}
	corpus := rankingPromotionCorpus{
		queries:   make([]rankingPromotionQuery, 0, len(rankingPromotionClusters)),
		documents: make([]rankingPromotionDocument, 0, len(rankingPromotionClusters)*len(variants)),
	}
	for queryIndex, cluster := range rankingPromotionClusters {
		query := rankingPromotionQueryToken(queryIndex)
		entry := rankingPromotionQuery{query: query, cluster: cluster}
		for variantIndex, variant := range variants {
			document := rankingPromotionDocumentFor(queryIndex, variantIndex, query, variant)
			corpus.documents = append(corpus.documents, document)
			switch variant.grade {
			case 0:
				entry.badURL = document.url
			case 1:
				entry.middleURL = document.url
			case 3:
				entry.goodURL = document.url
			}
		}
		corpus.queries = append(corpus.queries, entry)
	}

	return corpus
}

func rankingPromotionDocumentFor(
	queryIndex int,
	variantIndex int,
	query string,
	variant rankingPromotionVariant,
) rankingPromotionDocument {
	url := fmt.Sprintf(
		"https://%s-%02d.rank.invalid/document",
		variant.name,
		queryIndex,
	)
	text := rankingPromotionText(queryIndex, variantIndex, query, variant.stopwordInterval)
	title := alphabeticRankingToken('t', queryIndex*3+variantIndex)
	if variant.titleMatch {
		title = query
	}
	heading := alphabeticRankingToken('h', queryIndex*3+variantIndex)
	if variant.headingMatch {
		heading = query
	}
	digest := sha256.Sum256([]byte(url + "\x00" + text))

	return rankingPromotionDocument{
		url: url,
		document: yagocrawlcontract.DocumentIngest{
			CanonicalURL:  url,
			NormalizedURL: url,
			Title:         title,
			Headings:      []string{heading},
			ExtractedText: text,
			Language:      "en",
			ContentType:   "text/html",
			FetchStatus:   "200",
			ContentHash:   hex.EncodeToString(digest[:]),
		},
	}
}

func rankingPromotionText(
	queryIndex int,
	variantIndex int,
	query string,
	stopwordInterval int,
) string {
	words := make([]string, rankingPromotionDocumentWords)
	for wordIndex := range words {
		switch {
		case wordIndex == 2:
			words[wordIndex] = query
		case wordIndex%stopwordInterval == 0:
			words[wordIndex] = rankingPromotionStopwords[(wordIndex/stopwordInterval)%len(rankingPromotionStopwords)]
		default:
			identifier := ((queryIndex*3+variantIndex)*rankingPromotionDocumentWords + wordIndex)
			words[wordIndex] = alphabeticRankingToken('w', identifier)
		}
	}

	return strings.Join(words, " ")
}

func rankingPromotionQueryToken(value int) string {
	return string([]byte{
		'q',
		'a' + byte(value/26),
		'a' + byte(value%26),
		'z',
		'x',
		'v',
		'k',
		'n',
	})
}

func alphabeticRankingToken(prefix byte, value int) string {
	token := [8]byte{prefix, 'a', 'a', 'a', 'a', 'a', 'a', 'a'}
	for index := len(token) - 1; index > 0; index-- {
		token[index] = 'a' + byte(value%26)
		value /= 26
	}

	return string(token[:])
}

func ingestRankingPromotionCorpus(
	t *testing.T,
	ctx context.Context,
	address string,
	profileHandle string,
	corpus rankingPromotionCorpus,
) {
	t.Helper()
	connection, err := grpc.NewClient(
		address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("connect to node crawl exchange: %v", err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	client := crawlrpc.NewCrawlExchangeClient(connection)
	for _, document := range corpus.documents {
		batch, err := yagocrawlcontract.MarshalIngestBatch(yagocrawlcontract.IngestBatch{
			SourceURL:     document.url,
			ProfileHandle: profileHandle,
			Document:      document.document,
		})
		if err != nil {
			t.Fatalf("marshal ranking document %s: %v", document.url, err)
		}
		callContext, cancel := context.WithTimeout(ctx, 15*time.Second)
		_, err = client.SubmitIngest(
			callContext,
			&crawlrpc.IngestBatchMessage{BatchJson: batch},
		)
		cancel()
		if err != nil {
			t.Fatalf("ingest ranking document %s: %v", document.url, err)
		}
	}
}
