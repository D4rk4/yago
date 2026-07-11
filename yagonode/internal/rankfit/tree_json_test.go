package rankfit

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestHistogramModelJSONRoundTripIsDeterministic(t *testing.T) {
	definitions := []FeatureDefinition{
		{Name: "quality", Direction: FeatureIncreasing},
		{Name: "risk", Direction: FeatureDecreasing},
	}
	model := mustHistogramModel(
		t,
		definitions,
		0.125,
		histogramTree(
			"quality-risk",
			[]int{0, 1},
			histogramSplit(0, 0.5, histogramLeaf(-1), histogramLeaf(2)),
		),
		histogramTree("quality", []int{0}, histogramLeaf(0.25)),
	)
	encoded, err := json.Marshal(model)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	second, err := model.MarshalJSON()
	if err != nil || !reflect.DeepEqual(encoded, second) {
		t.Fatalf("deterministic Marshal = %s, %s, %v", encoded, second, err)
	}
	if !strings.Contains(string(encoded), histogramLambdaMARTFormat) {
		t.Fatalf("encoded model lacks format: %s", encoded)
	}
	var decoded HistogramLambdaMARTModel
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !reflect.DeepEqual(decoded, model) {
		t.Fatalf("decoded model differs: %#v, %#v", decoded, model)
	}
	zero := mustHistogramModel(t, definitionsForTest("only"), 0.1)
	if _, err := zero.MarshalJSON(); err != nil {
		t.Fatalf("MarshalJSON zero trees: %v", err)
	}
	if _, err := (HistogramLambdaMARTModel{}).MarshalJSON(); err == nil {
		t.Errorf("MarshalJSON accepted an invalid model")
	}
}

func TestHistogramModelJSONRejectsInvalidDocumentsWithoutMutation(t *testing.T) {
	model := mustHistogramModel(
		t,
		definitionsForTest("feature"),
		0.1,
		histogramTree("feature", []int{0}, histogramLeaf(1)),
	)
	before, err := json.Marshal(model)
	if err != nil {
		t.Fatalf("Marshal before: %v", err)
	}
	valid := `{"format":"yago-histogram-lambdamart-v1","features":[{"name":"x","direction":0}],"learning_rate":0.1,"trees":[]}`
	invalid := []string{
		`{`,
		`{"format":"yago-histogram-lambdamart-v1","features":[{"name":"x","direction":0}],"learning_rate":0.1,"trees":[],"unknown":1}`,
		valid + ` {}`,
		valid + ` x`,
		`{"format":"other","features":[{"name":"x","direction":0}],"learning_rate":0.1,"trees":[]}`,
		`{"format":"yago-histogram-lambdamart-v1","features":[],"learning_rate":0.1,"trees":[]}`,
		histogramInvalidTreeJSON(`{"leaf":0,"feature":0}`),
		histogramInvalidTreeJSON(`{}`),
		histogramInvalidTreeJSON(
			`{"feature":0,"threshold":0,"left":{},"right":{"leaf":1}}`,
		),
		histogramInvalidTreeJSON(
			`{"feature":0,"threshold":0,"left":{"leaf":-1},"right":{}}`,
		),
	}
	for _, document := range invalid {
		if err := json.Unmarshal([]byte(document), &model); err == nil {
			t.Errorf("Unmarshal(%q) succeeded", document)
		}
		after, err := json.Marshal(model)
		if err != nil || !reflect.DeepEqual(after, before) {
			t.Fatalf("failed Unmarshal changed model: %s, %v", after, err)
		}
	}
	for _, document := range []string{valid + ` {}`, valid + ` x`} {
		if err := model.UnmarshalJSON([]byte(document)); err == nil {
			t.Errorf("direct UnmarshalJSON(%q) succeeded", document)
		}
	}
}

func TestHistogramModelFormatsPreserveMissingEvidenceSemantics(t *testing.T) {
	legacyJSON := `{"format":"yago-histogram-lambdamart-v1","features":[{"name":"quality","direction":0}],"learning_rate":1,"trees":[{"interaction_group":"quality","allowed_feature_indices":[0],"root":{"feature":0,"threshold":0,"left":{"leaf":-1},"right":{"leaf":1}}}]}`
	var legacy HistogramLambdaMARTModel
	if err := json.Unmarshal([]byte(legacyJSON), &legacy); err != nil {
		t.Fatalf("decode legacy model: %v", err)
	}
	neutral := mustHistogramModel(
		t,
		definitionsForTest("quality"),
		1,
		histogramTree(
			"quality",
			[]int{0},
			histogramSplit(0, 0, histogramLeaf(-1), histogramLeaf(1)),
		),
	)
	group := mustQueryGroup(
		t,
		"missing",
		mustKnownRankingExample(t, "missing", []float64{0}, []bool{false}),
		mustKnownRankingExample(t, "low", []float64{-1}, []bool{true}),
		mustKnownRankingExample(t, "high", []float64{1}, []bool{true}),
	)
	assertMissingHistogramDecision(t, legacy, group, -1, false)
	assertMissingHistogramDecision(t, neutral, group, 0, true)
	encodedLegacy, err := json.Marshal(legacy)
	if err != nil || !strings.Contains(string(encodedLegacy), histogramLambdaMARTLegacyFormat) {
		t.Fatalf("legacy encoding = %s, %v", encodedLegacy, err)
	}
}

func assertMissingHistogramDecision(
	t *testing.T,
	model HistogramLambdaMARTModel,
	group QueryGroup,
	wantScore float64,
	wantTermination bool,
) {
	t.Helper()
	explanations, err := model.Explain(group)
	if err != nil {
		t.Fatalf("explain missing evidence: %v", err)
	}
	for _, explanation := range explanations {
		if explanation.DocumentIdentifier != "missing" {
			continue
		}
		decision := explanation.TreeContributions[0].Decisions[0]
		if explanation.Score != wantScore || decision.Known ||
			decision.TerminatedMissing != wantTermination {
			t.Fatalf("missing tree explanation = %#v", explanation)
		}

		return
	}
	t.Fatal("missing tree explanation was not found")
}

func TestHistogramTreeDocumentConversionErrors(t *testing.T) {
	zero := 0.0
	feature := 0
	threshold := 0.0
	leaf := histogramTreeNodeDocument{LeafValue: &zero}
	if _, err := histogramNodeFromDocument(histogramTreeNodeDocument{
		LeafValue: &zero,
		Left:      &leaf,
	}); err == nil {
		t.Errorf("ambiguous leaf was accepted")
	}
	if _, err := histogramNodeFromDocument(histogramTreeNodeDocument{}); err == nil {
		t.Errorf("incomplete split was accepted")
	}
	bad := histogramTreeNodeDocument{}
	if _, err := histogramTreesFromDocuments([]histogramRankingTreeDocument{{
		InteractionGroup:      "group",
		AllowedFeatureIndices: []int{0},
		Root: histogramTreeNodeDocument{
			FeatureIndex: &feature,
			Threshold:    &threshold,
			Left:         &leaf,
			Right:        &bad,
		},
	}}); err == nil {
		t.Errorf("invalid tree document was accepted")
	}
	if _, err := histogramNodeFromDocumentAtDepth(leaf, maximumHistogramDepth+1); err == nil {
		t.Errorf("deep tree document was accepted")
	}
	if _, err := histogramTreesFromDocuments(
		make([]histogramRankingTreeDocument, maximumHistogramTrees+1),
	); err == nil {
		t.Errorf("too many tree documents were accepted")
	}
}

func histogramInvalidTreeJSON(root string) string {
	return `{"format":"yago-histogram-lambdamart-v1","features":[{"name":"x","direction":0}],"learning_rate":0.1,"trees":[{"interaction_group":"x","allowed_feature_indices":[0],"root":` +
		root + `}]}`
}
