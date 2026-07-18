package pageindex

import (
	"maps"
	"strconv"

	"github.com/D4rk4/yago/yago-crawler/internal/pageparse"
	"github.com/D4rk4/yago/yago-crawler/internal/stopwords"
	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagomodel"
)

// BuildPostings turns a page's token stream into RWI postings, one per
// distinct content word. Stopwords produce no postings (SEARCH-39): the
// node's query side never requires them, so shipping their postings only
// bloated the index. Other tokens keep their position in the original
// stream, stopwords included, so proximity semantics are unchanged.
func BuildPostings(
	page pageparse.ParsedPage,
	stats pageparse.PageStats,
) []yagomodel.RWIPosting {
	frequency := make(map[string]int)
	firstPosition := make(map[string]int)
	order := make([]string, 0)
	for position, token := range stats.Tokens {
		if stopwords.IsStopword(token) {
			continue
		}
		if _, seen := frequency[token]; !seen {
			if len(order) == yagocrawlcontract.MaximumIngestPostings {
				continue
			}
			firstPosition[token] = position
			order = append(order, token)
			frequency[token] = 1

			continue
		}
		frequency[token]++
	}

	urlHash, _ := yagomodel.HashURL(page.URL)
	language := NormalizeLanguage(page.Language)

	shared := map[string]string{
		yagomodel.ColURLHash:  urlHash.String(),
		yagomodel.ColLanguage: language,
		yagomodel.ColDocType: strconv.FormatUint(
			uint64(yagomodel.DocTypeText),
			10,
		),
		yagomodel.ColTextWordCount: yagomodel.FormatRWICardinal(
			cardinalValue(len(stats.Tokens), maxUint16),
		),
		yagomodel.ColTitleWordCount: yagomodel.FormatRWICardinal(
			cardinalValue(len(stats.TitleTokens), maxUint8),
		),
		yagomodel.ColLocalLinkCount: yagomodel.FormatRWICardinal(
			cardinalValue(len(stats.LocalLinks), maxUint8),
		),
		yagomodel.ColExternalLinkCount: yagomodel.FormatRWICardinal(
			cardinalValue(len(stats.ExternalLinks), maxUint8),
		),
	}

	postings := make([]yagomodel.RWIPosting, 0, len(order))
	for _, token := range order {
		properties := make(map[string]string, len(shared)+2)
		maps.Copy(properties, shared)
		hits := cardinalValue(frequency[token], maxUint8)
		position := cardinalValue(firstPosition[token], maxUint16)
		properties[yagomodel.ColHitCount] = yagomodel.FormatRWICardinal(hits)
		properties[yagomodel.ColTextPosition] = yagomodel.FormatRWICardinal(position)
		postings = append(postings, yagomodel.RWIPosting{
			WordHash:   yagomodel.WordHash(token),
			Properties: properties,
		})
	}
	return postings
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
