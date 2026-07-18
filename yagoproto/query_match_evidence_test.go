package yagoproto

import (
	"encoding/base64"
	"maps"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
)

func TestQueryEvidenceRequestRoundTripAndBounds(t *testing.T) {
	request := SearchRequest{
		EvidenceVersion: QueryMatchEvidenceVersion,
		EvidenceTerms:   []string{" полномочия ", "權限"},
	}
	form := request.Form()
	if got := form[FieldQueryEvidenceTerm]; !reflect.DeepEqual(
		got,
		[]string{"полномочия", "權限"},
	) {
		t.Fatalf("evidence terms = %q", got)
	}
	parsed, err := ParseSearchRequest(t.Context(), form)
	if err != nil {
		t.Fatalf("parse request: %v", err)
	}
	if parsed.EvidenceVersion != QueryMatchEvidenceVersion ||
		!reflect.DeepEqual(parsed.EvidenceTerms, []string{"полномочия", "權限"}) {
		t.Fatalf("parsed evidence = %d %q", parsed.EvidenceVersion, parsed.EvidenceTerms)
	}

	tooMany := make([]string, maximumQueryEvidenceTerms+1)
	for index := range tooMany {
		tooMany[index] = "term"
	}
	totalTooLarge := make([]string, 17)
	for index := range totalTooLarge {
		totalTooLarge[index] = strings.Repeat("a", maximumQueryEvidenceTermBytes)
	}
	invalid := [][]string{
		nil,
		{""},
		{strings.Repeat("a", maximumQueryEvidenceTermBytes+1)},
		{"bad\xff"},
		tooMany,
		totalTooLarge,
	}
	for _, terms := range invalid {
		form := SearchRequest{
			EvidenceVersion: QueryMatchEvidenceVersion,
			EvidenceTerms:   terms,
		}.Form()
		if form.Has(FieldQueryEvidenceVersion) || form.Has(FieldQueryEvidenceTerm) {
			t.Fatalf("invalid evidence terms were encoded: %q", terms)
		}
	}
	unsupported := SearchRequest{EvidenceVersion: 2, EvidenceTerms: []string{"term"}}.Form()
	if unsupported.Has(FieldQueryEvidenceVersion) {
		t.Fatal("unsupported evidence version was encoded")
	}
	for _, version := range []string{"bad", "2"} {
		form := mapForm(
			FieldQueryEvidenceVersion, version,
			FieldQueryEvidenceTerm, "term",
		)
		parsed, err := ParseSearchRequest(t.Context(), form)
		if err != nil || parsed.EvidenceVersion != 0 || parsed.EvidenceTerms != nil {
			t.Fatalf(
				"version %q parsed as %d %q, err=%v",
				version,
				parsed.EvidenceVersion,
				parsed.EvidenceTerms,
				err,
			)
		}
	}
	invalidTerms := mapForm(
		FieldQueryEvidenceVersion, "1",
		FieldQueryEvidenceTerm, "",
	)
	parsed, err = ParseSearchRequest(t.Context(), invalidTerms)
	if err != nil || parsed.EvidenceVersion != 0 || parsed.EvidenceTerms != nil {
		t.Fatalf(
			"invalid terms parsed as %d %q, err=%v",
			parsed.EvidenceVersion,
			parsed.EvidenceTerms,
			err,
		)
	}
}

func TestSearchResponseQueryEvidenceRoundTrip(t *testing.T) {
	hash := yagomodel.WordHash("resource")
	row := evidenceResource(hash)
	evidence := validQueryMatchEvidenceFixture()
	response := SearchResponse{
		Count:            1,
		Resources:        []yagomodel.URIMetadataRow{row},
		ResourceEvidence: map[yagomodel.Hash]QueryMatchEvidence{hash: evidence},
		IndexCount:       map[yagomodel.Hash]int{},
		IndexAbstract:    map[yagomodel.Hash]string{},
	}
	parsed, err := ParseSearchResponse(response.Encode())
	if err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if !reflect.DeepEqual(parsed.ResourceEvidence, response.ResourceEvidence) {
		t.Fatalf("resource evidence = %#v", parsed.ResourceEvidence)
	}

	unknown := yagomodel.WordHash("unknown")
	encoded := SearchResponse{
		Count:            1,
		Resources:        []yagomodel.URIMetadataRow{row},
		ResourceEvidence: map[yagomodel.Hash]QueryMatchEvidence{unknown: evidence},
	}.Encode()
	if encoded[prefixResourceEvidence+unknown.String()] != "" {
		t.Fatal("evidence for an absent resource was encoded")
	}
}

func TestSearchResponseSkipsMalformedQueryEvidence(t *testing.T) {
	hash := yagomodel.WordHash("resource")
	row := evidenceResource(hash)
	base := SearchResponse{Count: 1, Resources: []yagomodel.URIMetadataRow{row}}.Encode()
	invalidJSON := base64.RawURLEncoding.EncodeToString([]byte("{"))
	invalidEvidence := base64.RawURLEncoding.EncodeToString(
		[]byte(`{"v":0,"a":"en"}`),
	)
	cases := []struct {
		key   string
		value string
	}{
		{prefixResourceEvidence + hash.String(), "%%%"},
		{prefixResourceEvidence + hash.String(), invalidJSON},
		{prefixResourceEvidence + hash.String(), invalidEvidence},
		{prefixResourceEvidence + "bad", invalidEvidence},
		{prefixResourceEvidence + yagomodel.WordHash("absent").String(), invalidEvidence},
		{
			prefixResourceEvidence + hash.String(),
			strings.Repeat("A", base64.RawURLEncoding.EncodedLen(maximumResourceEvidenceBytes)+1),
		},
	}
	for _, test := range cases {
		message := maps.Clone(base)
		message[test.key] = test.value
		parsed, err := ParseSearchResponse(message)
		if err != nil || parsed.ResourceEvidence != nil {
			t.Fatalf("malformed evidence parsed as %#v, err=%v", parsed.ResourceEvidence, err)
		}
	}
}

func TestQueryMatchEvidenceValidation(t *testing.T) {
	cases := append(
		invalidQueryMatchEvidenceEnvelopeMutations(),
		invalidQueryMatchEvidencePositionMutations()...,
	)
	for index, mutate := range cases {
		item := validQueryMatchEvidenceFixture()
		mutate(&item)
		if validQueryMatchEvidence(item) {
			t.Fatalf("invalid case %d accepted: %#v", index, item)
		}
	}
}

func invalidQueryMatchEvidenceEnvelopeMutations() []func(*QueryMatchEvidence) {
	return []func(*QueryMatchEvidence){
		func(item *QueryMatchEvidence) { item.Version = 0 },
		func(item *QueryMatchEvidence) { item.Analyzer = "" },
		func(item *QueryMatchEvidence) { item.Analyzer = "EN" },
		func(item *QueryMatchEvidence) { item.Analyzer = " en" },
		func(item *QueryMatchEvidence) { item.Analyzer = strings.Repeat("a", maximumResourceAnalyzerBytes+1) },
		func(item *QueryMatchEvidence) { item.Snippet = strings.Repeat("a", maximumResourceSnippetBytes+1) },
		func(item *QueryMatchEvidence) { item.Snippet = "bad\xff" },
		func(item *QueryMatchEvidence) {
			item.SnippetMatches = make([]QueryMatchRange, maximumResourceMatches+1)
		},
		func(item *QueryMatchEvidence) { item.BodyMatches = make([]QueryMatchRange, maximumResourceMatches+1) },
		func(item *QueryMatchEvidence) {
			item.FieldPositions = make([]QueryFieldPositions, maximumResourceEvidenceFields+1)
		},
	}
}

func invalidQueryMatchEvidencePositionMutations() []func(*QueryMatchEvidence) {
	return []func(*QueryMatchEvidence){
		func(item *QueryMatchEvidence) { item.SnippetMatches[0].Start = -1 },
		func(item *QueryMatchEvidence) { item.SnippetMatches[0].End = item.SnippetMatches[0].Start },
		func(item *QueryMatchEvidence) { item.SnippetMatches[0].End = len(item.Snippet) + 1 },
		func(item *QueryMatchEvidence) {
			item.SnippetMatches = []QueryMatchRange{{Start: 2, End: 3}, {Start: 1, End: 2}}
		},
		func(item *QueryMatchEvidence) {
			item.SnippetMatches = []QueryMatchRange{{Start: 0, End: 1}, {Start: 0, End: 1}}
		},
		func(item *QueryMatchEvidence) {
			item.Snippet = "權"
			item.SnippetMatches = []QueryMatchRange{{Start: 1, End: 2}}
		},
		func(item *QueryMatchEvidence) { item.BodyMatches[0].End = maximumResourceOffset + 1 },
		func(item *QueryMatchEvidence) { item.FieldPositions[0].Field = "unknown" },
		func(item *QueryMatchEvidence) {
			item.FieldPositions = append(item.FieldPositions, item.FieldPositions[0])
		},
		func(item *QueryMatchEvidence) {
			item.FieldPositions[0].Requirements = make(
				[]QueryRequirementPositions,
				maximumResourceRequirements+1,
			)
		},
		func(item *QueryMatchEvidence) { item.FieldPositions[0].Requirements[0].Ordinal = -1 },
		func(item *QueryMatchEvidence) {
			item.FieldPositions[0].Requirements[0].Ordinal = maximumResourceRequirements
		},
		func(item *QueryMatchEvidence) {
			item.FieldPositions[0].Requirements = append(
				item.FieldPositions[0].Requirements,
				item.FieldPositions[0].Requirements[0],
			)
		},
		func(item *QueryMatchEvidence) {
			item.FieldPositions[0].Requirements[0].Positions = make(
				[]int,
				maximumRequirementPositions+1,
			)
		},
		func(item *QueryMatchEvidence) { item.FieldPositions[0].Requirements[0].Positions = []int{0} },
		func(item *QueryMatchEvidence) { item.FieldPositions[0].Requirements[0].Positions = []int{2, 2} },
		func(item *QueryMatchEvidence) {
			item.FieldPositions[0].Requirements[0].Positions = []int{maximumResourceOffset + 1}
		},
		func(item *QueryMatchEvidence) {
			item.FieldPositions[0].Requirements = make([]QueryRequirementPositions, 5)
			for index := range item.FieldPositions[0].Requirements {
				positions := make([]int, maximumRequirementPositions)
				for position := range positions {
					positions[position] = position + 1
				}
				item.FieldPositions[0].Requirements[index] = QueryRequirementPositions{
					Ordinal:   index,
					Positions: positions,
				}
			}
		},
	}
}

func TestResourceEvidenceEncodingEnforcesWireBudgetAndResourceIdentity(t *testing.T) {
	hash := yagomodel.WordHash("resource")
	oversized := validQueryMatchEvidenceFixture()
	oversized.Snippet = strings.Repeat("\x01", maximumResourceSnippetBytes)
	oversized.SnippetMatches = make([]QueryMatchRange, maximumResourceMatches)
	oversized.BodyMatches = make([]QueryMatchRange, maximumResourceMatches)
	for index := range maximumResourceMatches {
		oversized.SnippetMatches[index] = QueryMatchRange{Start: index * 2, End: index*2 + 1}
		oversized.BodyMatches[index] = QueryMatchRange{
			Start: index*100000 + 1,
			End:   index*100000 + 2,
		}
	}
	positions := make([]QueryRequirementPositions, 4)
	for requirement := range positions {
		values := make([]int, maximumRequirementPositions)
		for index := range values {
			values[index] = requirement*maximumRequirementPositions + index + 1
		}
		positions[requirement] = QueryRequirementPositions{Ordinal: requirement, Positions: values}
	}
	oversized.FieldPositions[0].Requirements = positions
	oversized.RequirementOrdinals = []int{0, 1, 2, 3}
	if !validQueryMatchEvidence(oversized) {
		t.Fatal("oversized fixture must be structurally valid")
	}
	message := yagomodel.Message{}
	encodeResourceEvidence(
		message,
		[]yagomodel.URIMetadataRow{
			evidenceResource(hash),
			{Properties: map[string]string{"hash": "bad"}},
		},
		map[yagomodel.Hash]QueryMatchEvidence{hash: oversized},
	)
	if message[prefixResourceEvidence+hash.String()] != "" {
		t.Fatal("oversized resource evidence was encoded")
	}
}

func validQueryMatchEvidenceFixture() QueryMatchEvidence {
	return QueryMatchEvidence{
		Version:             QueryMatchEvidenceVersion,
		Analyzer:            "ru",
		RequirementOrdinals: []int{0},
		AbsentOrdinals:      []int{},
		Snippet:             "полномочий",
		SnippetMatches:      []QueryMatchRange{{Start: 0, End: len("полномочий")}},
		BodyMatches:         []QueryMatchRange{{Start: 100, End: 111}},
		FieldPositions: []QueryFieldPositions{{
			Field: "body",
			Requirements: []QueryRequirementPositions{{
				Ordinal: 0, Positions: []int{3, 9},
			}},
		}},
	}
}

func evidenceResource(hash yagomodel.Hash) yagomodel.URIMetadataRow {
	return yagomodel.URIMetadataRow{Properties: map[string]string{
		yagomodel.URLMetaHash: hash.String(),
	}}
}

func mapForm(values ...string) url.Values {
	form := make(url.Values, len(values)/2)
	for len(values) >= 2 {
		name, value := values[0], values[1]
		form[name] = append(form[name], value)
		values = values[2:]
	}

	return form
}
