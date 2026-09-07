package yacysearch

import (
	"cmp"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagoproto"
)

const publicSearchLimitCap = 10

func searchRequestFromValues(values url.Values) (searchcore.Request, error) {
	parsed, err := searchcore.ParsePublicTextQuery(values.Get(yagoproto.FieldQuery))
	if err != nil {
		return searchcore.Request{}, fmt.Errorf("public search request: %w", err)
	}
	// maximumRecords is the SRU name; count is the OpenSearch alias YaCy also honors.
	limit, err := optionalRequestInt(values, yagoproto.FieldMaximumRecords, yagoproto.FieldCount)
	if err != nil {
		return searchcore.Request{}, err
	}
	offset, err := optionalRequestInt(values, yagoproto.FieldStartRecord)
	if err != nil {
		return searchcore.Request{}, err
	}
	navigation := values.Get(yagoproto.FieldNavigation)

	req, err := searchcore.NormalizePublicRequest(searchcore.Request{
		Query:            values.Get(yagoproto.FieldQuery),
		Terms:            parsed.Terms,
		ExcludedTerms:    parsed.ExcludedTerms,
		Phrases:          parsed.Phrases(),
		Source:           searchcore.Source(values.Get(yagoproto.FieldResource)),
		Limit:            limit,
		Offset:           offset,
		ContentDomain:    searchcore.ContentDomain(values.Get(yagoproto.FieldContentDom)),
		Language:         cmp.Or(values.Get(yagoproto.FieldLanguage), parsed.Language),
		SiteHost:         cmp.Or(values.Get(yagoproto.FieldSiteHost), parsed.SiteHost),
		InURL:            parsed.InURL,
		TLD:              parsed.TLD,
		FileType:         cmp.Or(values.Get(yagoproto.FieldFileType), parsed.FileType),
		Author:           cmp.Or(values.Get(yagoproto.FieldAuthor), parsed.Author),
		URLMaskFilter:    values.Get(yagoproto.FieldURLMaskFilter),
		PreferMaskFilter: values.Get(yagoproto.FieldPreferMaskFilter),
		Verify:           searchcore.VerifyMode(values.Get(yagoproto.FieldVerify)),
		Navigation:       navigation,
		// A nav request asks the local index to tally facet counts; without it the
		// scan stays cheap.
		WithFacets: navigation != "" && !strings.EqualFold(navigation, "none"),
		SortByDate: parsed.SortByDate,
		Near:       parsed.Near,
	}, publicSearchLimitCap)
	if err != nil {
		return searchcore.Request{}, fmt.Errorf("public search request: %w", err)
	}

	return req, nil
}

// optionalRequestInt returns the first non-empty value among keys parsed as an
// integer, so a surface can accept several aliases for one numeric parameter.
func optionalRequestInt(values url.Values, keys ...string) (int, error) {
	for _, key := range keys {
		raw := values.Get(key)
		if raw == "" {
			continue
		}
		value, err := strconv.Atoi(raw)
		if err != nil {
			return 0, fmt.Errorf("%s: %w", key, err)
		}

		return value, nil
	}

	return 0, nil
}
