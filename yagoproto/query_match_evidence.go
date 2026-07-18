package yagoproto

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/D4rk4/yago/yagomodel"
)

const (
	QueryMatchEvidenceVersion        = 1
	maximumQueryEvidenceTerms        = 32
	maximumQueryEvidenceTermBytes    = 256
	maximumQueryEvidenceRequestBytes = 4 << 10
	maximumResourceEvidenceBytes     = 16 << 10
	maximumResourceEvidenceFields    = 5
	maximumResourceRequirements      = 32
	maximumResourcePositions         = 256
	maximumRequirementPositions      = 64
	maximumResourceMatches           = 128
	maximumResourceSnippetBytes      = 2 << 10
	maximumResourceOffset            = 1 << 30
	maximumResourceAnalyzerBytes     = 64
)

type QueryMatchRange struct {
	Start int `json:"s"`
	End   int `json:"e"`
}

type QueryRequirementPositions struct {
	Ordinal   int   `json:"o"`
	Positions []int `json:"p"`
}

type QueryFieldPositions struct {
	Field        string                      `json:"f"`
	Requirements []QueryRequirementPositions `json:"r"`
}

type QueryMatchEvidence struct {
	Version             int                   `json:"v"`
	Analyzer            string                `json:"a"`
	RequirementOrdinals []int                 `json:"q"`
	AbsentOrdinals      []int                 `json:"x"`
	Snippet             string                `json:"s,omitempty"`
	SnippetMatches      []QueryMatchRange     `json:"m,omitempty"`
	BodyMatches         []QueryMatchRange     `json:"b,omitempty"`
	FieldPositions      []QueryFieldPositions `json:"p,omitempty"`
}

func appendQueryEvidenceRequest(form url.Values, version int, terms []string) {
	bounded, valid := validatedQueryEvidenceTerms(terms)
	if version != QueryMatchEvidenceVersion || !valid {
		return
	}
	putInt(form, FieldQueryEvidenceVersion, version)
	for _, term := range bounded {
		form.Add(FieldQueryEvidenceTerm, term)
	}
}

func parseQueryEvidenceRequest(form url.Values) (int, []string) {
	version, err := optionalInt(FieldQueryEvidenceVersion, form.Get(FieldQueryEvidenceVersion))
	if err != nil || version != QueryMatchEvidenceVersion {
		return 0, nil
	}
	terms, valid := validatedQueryEvidenceTerms(form[FieldQueryEvidenceTerm])
	if !valid {
		return 0, nil
	}

	return version, terms
}

func validatedQueryEvidenceTerms(terms []string) ([]string, bool) {
	if len(terms) == 0 || len(terms) > maximumQueryEvidenceTerms {
		return nil, false
	}
	bounded := make([]string, len(terms))
	total := 0
	for index, term := range terms {
		term = strings.TrimSpace(term)
		if term == "" || !utf8.ValidString(term) || len(term) > maximumQueryEvidenceTermBytes {
			return nil, false
		}
		total += len(term)
		if total > maximumQueryEvidenceRequestBytes {
			return nil, false
		}
		bounded[index] = term
	}

	return bounded, true
}

func encodeResourceEvidence(
	message yagomodel.Message,
	resources []yagomodel.URIMetadataRow,
	evidence map[yagomodel.Hash]QueryMatchEvidence,
) {
	allowed := resourceEvidenceHashes(resources)
	for hash, item := range evidence {
		if _, found := allowed[hash]; !found || !validQueryMatchEvidence(item) {
			continue
		}
		encoded, _ := json.Marshal(item)
		if len(encoded) > maximumResourceEvidenceBytes {
			continue
		}
		message[prefixResourceEvidence+hash.String()] = base64.RawURLEncoding.EncodeToString(
			encoded,
		)
	}
}

func parseResourceEvidence(
	message yagomodel.Message,
	resources []yagomodel.URIMetadataRow,
) map[yagomodel.Hash]QueryMatchEvidence {
	allowed := resourceEvidenceHashes(resources)
	parsed := make(map[yagomodel.Hash]QueryMatchEvidence)
	for key, value := range message {
		if !strings.HasPrefix(key, prefixResourceEvidence) ||
			len(value) > base64.RawURLEncoding.EncodedLen(maximumResourceEvidenceBytes) {
			continue
		}
		hash, err := yagomodel.ParseHash(strings.TrimPrefix(key, prefixResourceEvidence))
		if err != nil {
			continue
		}
		if _, found := allowed[hash]; !found {
			continue
		}
		decoded, err := base64.RawURLEncoding.DecodeString(value)
		if err != nil {
			continue
		}
		var item QueryMatchEvidence
		if json.Unmarshal(decoded, &item) != nil || !validQueryMatchEvidence(item) {
			continue
		}
		parsed[hash] = item
	}
	if len(parsed) == 0 {
		return nil
	}

	return parsed
}

func resourceEvidenceHashes(resources []yagomodel.URIMetadataRow) map[yagomodel.Hash]struct{} {
	hashes := make(map[yagomodel.Hash]struct{}, len(resources))
	for _, resource := range resources {
		hash, err := resource.URLHash()
		if err == nil {
			hashes[yagomodel.Hash(hash)] = struct{}{}
		}
	}

	return hashes
}

func validQueryMatchEvidence(evidence QueryMatchEvidence) bool {
	if !validQueryMatchEvidenceEnvelope(evidence) {
		return false
	}
	if !validQueryMatchRanges(
		evidence.SnippetMatches,
		len(evidence.Snippet),
		evidence.Snippet,
	) || !validQueryMatchRanges(evidence.BodyMatches, maximumResourceOffset, "") {
		return false
	}

	return validQueryMatchEvidenceFields(evidence.FieldPositions) &&
		validQueryMatchEvidencePartition(evidence)
}

func validResourceAnalyzer(analyzer string) bool {
	if analyzer == "" || len(analyzer) > maximumResourceAnalyzerBytes ||
		strings.TrimSpace(analyzer) != analyzer {
		return false
	}
	for _, character := range analyzer {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' ||
			character == '-' || character == '_' {
			continue
		}
		return false
	}

	return true
}

func validEvidenceField(field string) bool {
	switch field {
	case "title", "headings", "anchors", "body", "url":
		return true
	default:
		return false
	}
}

func validQueryMatchRanges(ranges []QueryMatchRange, boundary int, text string) bool {
	previous := QueryMatchRange{}
	for index, match := range ranges {
		if match.Start < 0 || match.End <= match.Start || match.End > boundary ||
			index > 0 && (match.Start < previous.Start ||
				match.Start == previous.Start && match.End <= previous.End) {
			return false
		}
		if text != "" && (!utf8.RuneStart(text[match.Start]) ||
			match.End < len(text) && !utf8.RuneStart(text[match.End])) {
			return false
		}
		previous = match
	}

	return true
}

func validEvidencePositions(positions []int) bool {
	previous := 0
	for _, position := range positions {
		if position <= previous || position > maximumResourceOffset {
			return false
		}
		previous = position
	}

	return true
}
