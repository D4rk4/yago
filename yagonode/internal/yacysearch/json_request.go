package yacysearch

import (
	"fmt"
	"net/url"
	"strconv"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagoproto"
)

const publicSearchLimitCap = 10

func searchRequestFromValues(values url.Values) (searchcore.Request, error) {
	parsed := searchcore.ParseTextQuery(values.Get(yagoproto.FieldQuery))
	limit, err := optionalRequestInt(values, yagoproto.FieldMaximumRecords)
	if err != nil {
		return searchcore.Request{}, err
	}
	offset, err := optionalRequestInt(values, yagoproto.FieldStartRecord)
	if err != nil {
		return searchcore.Request{}, err
	}

	req, err := searchcore.NormalizePublicRequest(searchcore.Request{
		Query:            values.Get(yagoproto.FieldQuery),
		Terms:            parsed.Terms,
		ExcludedTerms:    parsed.ExcludedTerms,
		Phrases:          parsed.Phrases(),
		Source:           searchcore.Source(values.Get(yagoproto.FieldResource)),
		Limit:            limit,
		Offset:           offset,
		ContentDomain:    searchcore.ContentDomain(values.Get(yagoproto.FieldContentDom)),
		Language:         firstNonEmpty(values.Get(yagoproto.FieldLanguage), parsed.Language),
		SiteHost:         firstNonEmpty(values.Get(yagoproto.FieldSiteHost), parsed.SiteHost),
		InURL:            parsed.InURL,
		TLD:              parsed.TLD,
		FileType:         firstNonEmpty(values.Get(yagoproto.FieldFileType), parsed.FileType),
		URLMaskFilter:    values.Get(yagoproto.FieldURLMaskFilter),
		PreferMaskFilter: values.Get(yagoproto.FieldPreferMaskFilter),
		Verify:           searchcore.VerifyMode(values.Get(yagoproto.FieldVerify)),
		Navigation:       values.Get(yagoproto.FieldNavigation),
		SortByDate:       parsed.SortByDate,
		Near:             parsed.Near,
	}, publicSearchLimitCap)
	if err != nil {
		return searchcore.Request{}, fmt.Errorf("public search request: %w", err)
	}

	return req, nil
}

func optionalRequestInt(values url.Values, key string) (int, error) {
	raw := values.Get(key)
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}

	return value, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}

	return ""
}
