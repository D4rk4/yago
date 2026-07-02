package yacyproto

import (
	"context"
	"net/url"
	"strings"

	"github.com/D4rk4/yago/yacymodel"
)

type SearchRequest struct {
	NetworkName      string
	MySeed           yacymodel.Optional[yacymodel.Seed]
	Query            []yacymodel.Hash
	Exclude          []yacymodel.Hash
	URLs             []yacymodel.Hash
	Count            int
	Time             int
	MaxDist          int
	Partitions       int
	Abstracts        SearchAbstracts
	ContentDom       SearchContentDomain
	StrictContentDom bool
	TimezoneOffset   int
	Language         string
	Modifier         string
	Prefer           string
	Filter           string
	Constraint       string
	Profile          string
	SiteHost         string
	SiteHash         string
	Author           string
	Collection       string
	FileType         string
	Protocol         string
}

type SearchResponse struct {
	ResponseHeader
	SearchTime    int
	References    string
	JoinCount     int
	Count         int
	Resources     []yacymodel.URIMetadataRow
	IndexCount    map[yacymodel.Hash]int
	IndexAbstract map[yacymodel.Hash]string
}

func (r SearchRequest) Form() url.Values {
	form := url.Values{}
	putString(form, FieldNetworkName, r.NetworkName)
	if seed, ok := r.MySeed.Get(); ok {
		putString(form, FieldMySeed, yacymodel.EncodeCompactWireForm(seed.String()))
	}
	putString(form, FieldQuery, concatHashes(r.Query))
	putString(form, FieldExclude, concatHashes(r.Exclude))
	putString(form, FieldURLs, concatHashes(r.URLs))
	putIntOptional(form, FieldCount, r.Count)
	putIntOptional(form, FieldTime, r.Time)
	putIntOptional(form, FieldMaxDist, r.MaxDist)
	putIntOptional(form, FieldPartitions, r.Partitions)
	putString(form, FieldAbstracts, string(r.Abstracts))
	putString(form, FieldContentDom, string(r.ContentDom))
	putBoolOptional(form, FieldStrictContentDom, r.StrictContentDom)
	putIntOptional(form, FieldTimezoneOffset, r.TimezoneOffset)
	putString(form, FieldLanguage, r.Language)
	putString(form, FieldModifier, r.Modifier)
	putString(form, FieldPrefer, r.Prefer)
	putString(form, FieldFilter, r.Filter)
	putString(form, FieldConstraint, r.Constraint)
	putString(form, FieldProfile, r.Profile)
	putString(form, FieldSiteHost, r.SiteHost)
	putString(form, FieldSiteHash, r.SiteHash)
	putString(form, FieldAuthor, r.Author)
	putString(form, FieldCollection, r.Collection)
	putString(form, FieldFileType, r.FileType)
	putString(form, FieldProtocol, r.Protocol)

	return form
}

func ParseSearchRequest(ctx context.Context, form url.Values) (SearchRequest, error) {
	counts, err := searchRequestCounts(form)
	if err != nil {
		return SearchRequest{}, err
	}

	req := SearchRequest{
		NetworkName:      form.Get(FieldNetworkName),
		Count:            counts.count,
		Time:             counts.time,
		MaxDist:          counts.maxDist,
		Partitions:       counts.partitions,
		StrictContentDom: counts.strictContentDom,
		TimezoneOffset:   counts.timezoneOffset,
		Language:         form.Get(FieldLanguage),
		Modifier:         form.Get(FieldModifier),
		Prefer:           form.Get(FieldPrefer),
		Filter:           form.Get(FieldFilter),
		Constraint:       form.Get(FieldConstraint),
		Profile:          form.Get(FieldProfile),
		SiteHost:         form.Get(FieldSiteHost),
		SiteHash:         form.Get(FieldSiteHash),
		Author:           form.Get(FieldAuthor),
		Collection:       form.Get(FieldCollection),
		FileType:         form.Get(FieldFileType),
		Protocol:         form.Get(FieldProtocol),
	}

	if raw := form.Get(FieldMySeed); raw != "" {
		seed, err := decodeSeed(ctx, raw)
		if err != nil {
			return SearchRequest{}, err
		}
		req.MySeed = yacymodel.Some(seed)
	}

	req.Query, err = splitSearchHashes(FieldQuery, form.Get(FieldQuery))
	if err != nil {
		return SearchRequest{}, err
	}

	req.Exclude, err = splitSearchHashes(FieldExclude, form.Get(FieldExclude))
	if err != nil {
		return SearchRequest{}, err
	}

	req.URLs, err = splitSearchHashes(FieldURLs, form.Get(FieldURLs))
	if err != nil {
		return SearchRequest{}, err
	}

	req.Abstracts, err = parseSearchAbstracts(form.Get(FieldAbstracts))
	if err != nil {
		return SearchRequest{}, err
	}

	req.ContentDom, err = parseSearchContentDomain(form.Get(FieldContentDom))
	if err != nil {
		return SearchRequest{}, err
	}

	return req, nil
}

type searchCounts struct {
	count            int
	time             int
	maxDist          int
	partitions       int
	timezoneOffset   int
	strictContentDom bool
}

func searchRequestCounts(form url.Values) (searchCounts, error) {
	var (
		counts searchCounts
		err    error
	)

	if counts.count, err = optionalInt(FieldCount, form.Get(FieldCount)); err != nil {
		return searchCounts{}, err
	}

	if counts.time, err = optionalInt(FieldTime, form.Get(FieldTime)); err != nil {
		return searchCounts{}, err
	}

	if counts.maxDist, err = optionalInt(FieldMaxDist, form.Get(FieldMaxDist)); err != nil {
		return searchCounts{}, err
	}

	if counts.partitions, err = optionalInt(
		FieldPartitions,
		form.Get(FieldPartitions),
	); err != nil {
		return searchCounts{}, err
	}

	if counts.timezoneOffset, err = optionalInt(
		FieldTimezoneOffset,
		form.Get(FieldTimezoneOffset),
	); err != nil {
		return searchCounts{}, err
	}

	if counts.strictContentDom, err = optionalBool(
		FieldStrictContentDom,
		form.Get(FieldStrictContentDom),
	); err != nil {
		return searchCounts{}, err
	}

	return counts, nil
}

func (r SearchResponse) Encode() yacymodel.Message {
	msg := yacymodel.Message{}
	setInt(msg, FieldSearchTime, r.SearchTime)
	setString(msg, FieldReferences, r.References)
	setInt(msg, FieldJoinCount, r.JoinCount)
	setInt(msg, FieldCount, r.Count)
	for i, row := range r.Resources {
		setString(msg, indexedKey(prefixResource, i), row.String())
	}
	for hash, count := range r.IndexCount {
		setInt(msg, prefixIndexCount+hash.String(), count)
	}
	for hash, abstract := range r.IndexAbstract {
		setString(msg, prefixIndexAbstract+hash.String(), abstract)
	}

	return msg
}

func ParseSearchResponse(m yacymodel.Message) (SearchResponse, error) {
	header, err := parseResponseHeader(m)
	if err != nil {
		return SearchResponse{}, err
	}

	resp := SearchResponse{
		ResponseHeader: header,
		References:     m[FieldReferences],
	}

	if resp.SearchTime, err = optionalInt(FieldSearchTime, m[FieldSearchTime]); err != nil {
		return SearchResponse{}, err
	}

	if resp.JoinCount, err = optionalInt(FieldJoinCount, m[FieldJoinCount]); err != nil {
		return SearchResponse{}, err
	}

	if resp.Count, err = searchResponseCount(m); err != nil {
		return SearchResponse{}, err
	}

	resp.Resources = parseSearchResources(m, resp.Count)

	if resp.IndexCount, resp.IndexAbstract, err = parseSearchIndexes(m); err != nil {
		return SearchResponse{}, err
	}

	return resp, nil
}

func searchResponseCount(m yacymodel.Message) (int, error) {
	if raw := m[FieldCount]; raw != "" {
		return optionalInt(FieldCount, raw)
	}

	return optionalInt(FieldLinkCount, m[FieldLinkCount])
}

func parseSearchResources(m yacymodel.Message, count int) []yacymodel.URIMetadataRow {
	var rows []yacymodel.URIMetadataRow
	if count > 0 {
		for i := range count {
			raw, ok := m[indexedKey(prefixResource, i)]
			if !ok {
				continue
			}
			row, err := yacymodel.ParseURIMetadataRow(raw)
			if err != nil {
				continue
			}
			rows = append(rows, row)
		}
		return rows
	}

	for i := 0; ; i++ {
		raw, ok := m[indexedKey(prefixResource, i)]
		if !ok {
			return rows
		}

		row, err := yacymodel.ParseURIMetadataRow(raw)
		if err != nil {
			continue
		}

		rows = append(rows, row)
	}
}

func parseSearchIndexes(
	m yacymodel.Message,
) (map[yacymodel.Hash]int, map[yacymodel.Hash]string, error) {
	var (
		counts    map[yacymodel.Hash]int
		abstracts map[yacymodel.Hash]string
	)

	for key, value := range m {
		switch {
		case strings.HasPrefix(key, prefixIndexCount):
			hash, err := parseHashField("search response", key, key[len(prefixIndexCount):])
			if err != nil {
				return nil, nil, err
			}

			count, err := readInt(key, value)
			if err != nil {
				return nil, nil, err
			}

			if counts == nil {
				counts = map[yacymodel.Hash]int{}
			}

			counts[hash] = count
		case strings.HasPrefix(key, prefixIndexAbstract):
			hash, err := parseHashField("search response", key, key[len(prefixIndexAbstract):])
			if err != nil {
				return nil, nil, err
			}

			if abstracts == nil {
				abstracts = map[yacymodel.Hash]string{}
			}

			abstracts[hash] = value
		}
	}

	return counts, abstracts, nil
}
