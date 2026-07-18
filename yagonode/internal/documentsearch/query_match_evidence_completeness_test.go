package documentsearch

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagoproto"
)

func TestProtocolPositionsReserveOneWitnessPerPresentRequirement(t *testing.T) {
	positions := make(map[int][]int, 32)
	for ordinal := range 32 {
		positions[ordinal] = make([]int, maximumProtocolRequirementPositions)
		for index := range positions[ordinal] {
			positions[ordinal][index] = ordinal*maximumProtocolRequirementPositions + index + 1
		}
	}
	fields := ProtocolQueryFieldPositions(map[string]map[int][]int{"title": positions})
	present := make(map[int]struct{}, 32)
	total := 0
	for _, field := range fields {
		for _, requirement := range field.Requirements {
			present[requirement.Ordinal] = struct{}{}
			total += len(requirement.Positions)
		}
	}
	if total != MaximumQueryMatchEvidencePositions || len(present) != 32 {
		t.Fatalf("positions=%d present=%d fields=%#v", total, len(present), fields)
	}
}

func TestProtocolPositionsRemainBoundedWithExcessOrdinals(t *testing.T) {
	positions := make(map[int][]int, MaximumQueryMatchEvidencePositions+1)
	for ordinal := range MaximumQueryMatchEvidencePositions + 1 {
		positions[ordinal] = []int{ordinal + 1}
	}
	fields := ProtocolQueryFieldPositions(map[string]map[int][]int{"title": positions})
	total := 0
	for _, field := range fields {
		for _, requirement := range field.Requirements {
			total += len(requirement.Positions)
		}
	}
	if total != MaximumQueryMatchEvidencePositions {
		t.Fatalf("positions = %d", total)
	}
}

func TestEvidenceSourcePublishesAnalyzerRelevantAndAbsentOrdinals(t *testing.T) {
	rawURL := "https://example.test/alpha"
	hash, err := yagomodel.HashURL(rawURL)
	if err != nil {
		t.Fatalf("HashURL: %v", err)
	}
	source := queryMatchEvidenceSource{
		documents: completenessDocumentDirectory{document: documentstore.Document{
			NormalizedURL: rawURL,
			Title:         "alpha",
			Language:      "en",
		}},
		analyzer: testQueryMatchEvidenceAnalyzer{},
	}
	request := yagoproto.SearchRequest{
		Query: []yagomodel.Hash{
			yagomodel.WordHash("alpha"),
			yagomodel.WordHash("beta"),
			yagomodel.WordHash("gamma"),
		},
		EvidenceVersion: yagoproto.QueryMatchEvidenceVersion,
		EvidenceTerms:   []string{"alpha", "beta", "gamma"},
	}
	resources := []yagomodel.URIMetadataRow{{Properties: map[string]string{
		yagomodel.URLMetaHash: hash.String(),
		yagomodel.URLMetaURL:  yagomodel.EncodeBase64WireForm(rawURL),
	}}}
	item, found := source.resources(t.Context(), request, resources)[hash.Hash()]
	if !found || len(item.RequirementOrdinals) != 3 ||
		len(item.AbsentOrdinals) != 2 || item.AbsentOrdinals[0] != 1 ||
		item.AbsentOrdinals[1] != 2 {
		t.Fatalf("evidence = %#v", item)
	}
}

type completenessDocumentDirectory struct {
	document documentstore.Document
}

func (directory completenessDocumentDirectory) Document(
	context.Context,
	string,
) (documentstore.Document, bool, error) {
	return directory.document, true, nil
}

func (directory completenessDocumentDirectory) Count(context.Context) (int, error) {
	return 1, nil
}
