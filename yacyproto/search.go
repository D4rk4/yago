package yacyproto

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

// SearchRequest is the GET|POST /yacy/search.html request: a remote search over
// DHT word hashes. Query holds the concatenated 12-character word hashes.
type SearchRequest struct {
	NetworkName      string
	MySeed           yacymodel.Seed
	Query            []yacymodel.Hash
	Exclude          string
	URLs             string
	Count            int
	Time             int
	MaxDist          int
	Partitions       int
	Abstracts        string
	ContentDom       string
	StrictContentDom string
	TimezoneOffset   string
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

// SearchResponse is the /yacy/search.html response. Resources are the returned
// URL rows; IndexCount and IndexAbstract are keyed by queried word hash.
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

// Form renders the request as HTTP form fields.
func (r SearchRequest) Form() url.Values {
	form := url.Values{}
	putString(form, FieldNetworkName, r.NetworkName)
	if r.MySeed != nil {
		putString(form, FieldMySeed, yacymodel.EncodeSeedWireForm(r.MySeed.String()))
	}
	putString(form, FieldQuery, concatHashes(r.Query))
	putString(form, FieldExclude, r.Exclude)
	putString(form, FieldURLs, r.URLs)
	putIntOptional(form, FieldCount, r.Count)
	putIntOptional(form, FieldTime, r.Time)
	putIntOptional(form, FieldMaxDist, r.MaxDist)
	putIntOptional(form, FieldPartitions, r.Partitions)
	putString(form, FieldAbstracts, r.Abstracts)
	putString(form, FieldContentDom, r.ContentDom)
	putString(form, FieldStrictContentDom, r.StrictContentDom)
	putString(form, FieldTimezoneOffset, r.TimezoneOffset)
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

// ParseSearchRequest reads a SearchRequest from HTTP form fields.
func ParseSearchRequest(form url.Values) (SearchRequest, error) {
	counts, err := searchRequestCounts(form)
	if err != nil {
		return SearchRequest{}, err
	}

	req := SearchRequest{
		NetworkName:      form.Get(FieldNetworkName),
		Exclude:          form.Get(FieldExclude),
		URLs:             form.Get(FieldURLs),
		Count:            counts.count,
		Time:             counts.time,
		MaxDist:          counts.maxDist,
		Partitions:       counts.partitions,
		Abstracts:        form.Get(FieldAbstracts),
		ContentDom:       form.Get(FieldContentDom),
		StrictContentDom: form.Get(FieldStrictContentDom),
		TimezoneOffset:   form.Get(FieldTimezoneOffset),
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
		req.MySeed, err = decodeSeed(raw)
		if err != nil {
			return SearchRequest{}, err
		}
	}

	req.Query, err = splitConcatHashes("search request", FieldQuery, form.Get(FieldQuery))
	if err != nil {
		return SearchRequest{}, err
	}

	return req, nil
}

type searchCounts struct {
	count      int
	time       int
	maxDist    int
	partitions int
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

	return counts, nil
}

// Encode renders the response as a key=value message.
func (r SearchResponse) Encode() yacymodel.Message {
	msg := yacymodel.Message{}
	r.write(msg)
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

// ParseSearchResponse reads a SearchResponse from key=value lines.
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

	if resp.Count, err = optionalInt(FieldCount, m[FieldCount]); err != nil {
		return SearchResponse{}, err
	}

	if resp.Resources, err = parseSearchResources(m); err != nil {
		return SearchResponse{}, err
	}

	if resp.IndexCount, resp.IndexAbstract, err = parseSearchIndexes(m); err != nil {
		return SearchResponse{}, err
	}

	return resp, nil
}

func parseSearchResources(m yacymodel.Message) ([]yacymodel.URIMetadataRow, error) {
	var rows []yacymodel.URIMetadataRow
	for i := 0; ; i++ {
		raw, ok := m[indexedKey(prefixResource, i)]
		if !ok {
			return rows, nil
		}

		row, err := yacymodel.ParseURIMetadataRow(raw)
		if err != nil {
			return nil, fmt.Errorf(
				"search response %s: %w", indexedKey(prefixResource, i), err,
			)
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
