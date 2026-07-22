package searchindex

import (
	"strconv"
	"strings"
	"time"
)

func cacheKey(req SearchRequest) string {
	var builder strings.Builder
	writeCacheField(&builder, req.Query)
	writeCacheStrings(&builder, req.ExcludeTerms)
	writeCacheStrings(&builder, req.Phrases)
	writeCacheField(&builder, strconv.Itoa(req.MaxResults))
	writeCacheField(&builder, strconv.FormatBool(req.IncludeRaw))
	writeCacheField(&builder, strconv.FormatBool(req.SafeSearch))
	writeCacheField(&builder, req.SiteHost)
	writeCacheStrings(&builder, req.IncludeDomain)
	writeCacheStrings(&builder, req.ExcludeDomain)
	writeCacheField(&builder, req.Language)
	writeCacheTime(&builder, req.Since)
	writeCacheTime(&builder, req.Until)
	writeCacheWeights(&builder, req.Weights.orDefault())
	writeCacheField(&builder, strconv.FormatBool(req.Explain))
	writeCacheField(&builder, strconv.FormatBool(req.IncludeFieldScores))
	writeCacheField(&builder, strconv.FormatBool(req.IncludePositions))
	writeCacheField(&builder, strconv.FormatBool(req.CandidateOnly))
	writeCacheField(&builder, strconv.FormatBool(req.Fuzzy))
	writeCacheField(&builder, strconv.FormatBool(req.Relaxed))
	writeCacheField(&builder, req.Author)
	writeCacheStrings(&builder, req.Terms)
	writeCacheField(&builder, strconv.FormatBool(req.Near))
	writeCacheStrings(&builder, req.ExpansionTerms)
	writeCacheField(&builder, strconv.Itoa(req.MinimumTermMatches))
	writeCacheField(&builder, strconv.FormatBool(req.WithFacets))
	writeCacheField(&builder, req.ContentDomain)
	writeCacheTime(&builder, req.MinDate)
	writeCacheTime(&builder, req.MaxDate)
	writeCacheField(&builder, req.FileType)
	writeCacheField(&builder, req.InURL)
	writeCacheField(&builder, req.TLD)

	return builder.String()
}

func writeCacheStrings(builder *strings.Builder, values []string) {
	writeCacheField(builder, strconv.Itoa(len(values)))
	for _, value := range values {
		writeCacheField(builder, value)
	}
}

func writeCacheTime(builder *strings.Builder, value time.Time) {
	writeCacheField(builder, value.UTC().Format(time.RFC3339Nano))
}

func writeCacheWeights(builder *strings.Builder, weights RankingWeights) {
	for _, definition := range rankingWeightDefinitions {
		value, _ := weights.Value(definition.Key)
		writeCacheField(builder, strconv.FormatFloat(value, 'g', -1, 64))
	}
}

func writeCacheField(builder *strings.Builder, value string) {
	builder.WriteString(strconv.Itoa(len(value)))
	builder.WriteByte(':')
	builder.WriteString(value)
}
