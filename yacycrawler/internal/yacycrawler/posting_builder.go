package yacycrawler

import (
	"fmt"
	"maps"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

func BuildPostings(page ParsedPage) ([]yacymodel.RWIPosting, error) {
	return buildPostings(page, BuildPageStats(page))
}

func buildPostings(page ParsedPage, stats PageStats) ([]yacymodel.RWIPosting, error) {
	frequency := make(map[string]int)
	firstPosition := make(map[string]int)
	order := make([]string, 0)
	for position, token := range stats.Tokens {
		if _, seen := frequency[token]; !seen {
			firstPosition[token] = position
			order = append(order, token)
		}
		frequency[token]++
	}

	urlHash, err := yacymodel.HashURL(page.URL)
	if err != nil {
		return nil, fmt.Errorf("hash url: %w", err)
	}
	language := NormalizeLanguage(page.Language)

	shared := map[string]string{
		yacymodel.ColURLHash:  urlHash.String(),
		yacymodel.ColLanguage: language,
		yacymodel.ColTextWordCount: yacymodel.FormatRWICardinal(
			cardinalValue(len(stats.Tokens), maxUint16),
		),
		yacymodel.ColTitleWordCount: yacymodel.FormatRWICardinal(
			cardinalValue(len(stats.TitleTokens), maxUint8),
		),
		yacymodel.ColLocalLinkCount: yacymodel.FormatRWICardinal(
			cardinalValue(len(stats.LocalLinks), maxUint8),
		),
		yacymodel.ColExternalLinkCount: yacymodel.FormatRWICardinal(
			cardinalValue(len(stats.ExternalLinks), maxUint8),
		),
	}

	postings := make([]yacymodel.RWIPosting, 0, len(order))
	for _, token := range order {
		properties := make(map[string]string, len(shared)+2)
		maps.Copy(properties, shared)
		hits := cardinalValue(frequency[token], maxUint8)
		position := cardinalValue(firstPosition[token], maxUint16)
		properties[yacymodel.ColHitCount] = yacymodel.FormatRWICardinal(hits)
		properties[yacymodel.ColTextPosition] = yacymodel.FormatRWICardinal(position)
		postings = append(postings, yacymodel.RWIPosting{
			WordHash:   yacymodel.WordHash(token),
			Properties: properties,
		})
	}
	return postings, nil
}

const (
	maxUint8  = 0xff
	maxUint16 = 0xffff
)

func cardinalValue(value, maximum int) uint64 {
	if value < 0 {
		return 0
	}
	if value > maximum {
		value = maximum
	}
	return uint64(value)
}
