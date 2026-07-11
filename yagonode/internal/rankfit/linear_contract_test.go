package rankfit

import (
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"
)

func mustLinearFeatureVector(t *testing.T, values ...float64) FeatureVector {
	t.Helper()
	vector, err := NewFeatureVector(values)
	if err != nil {
		t.Fatalf("NewFeatureVector: %v", err)
	}

	return vector
}

func mustKnownFeatureVector(t *testing.T, values []float64, known []bool) FeatureVector {
	t.Helper()
	vector, err := NewFeatureVectorWithKnownValues(values, known)
	if err != nil {
		t.Fatalf("NewFeatureVectorWithKnownValues: %v", err)
	}

	return vector
}

func mustKnownRankingExample(
	t *testing.T,
	documentIdentifier string,
	values []float64,
	known []bool,
) RankingExample {
	t.Helper()
	example, err := NewRankingExample(
		documentIdentifier,
		0,
		mustKnownFeatureVector(t, values, known),
	)
	if err != nil {
		t.Fatalf("NewRankingExample: %v", err)
	}

	return example
}

func mustRankingExample(
	t *testing.T,
	documentIdentifier string,
	relevance int,
	values ...float64,
) RankingExample {
	t.Helper()
	example, err := NewRankingExample(
		documentIdentifier,
		relevance,
		mustLinearFeatureVector(t, values...),
	)
	if err != nil {
		t.Fatalf("NewRankingExample: %v", err)
	}

	return example
}

func mustQueryGroup(t *testing.T, identifier string, examples ...RankingExample) QueryGroup {
	t.Helper()
	group, err := NewQueryGroup(identifier, examples)
	if err != nil {
		t.Fatalf("NewQueryGroup: %v", err)
	}

	return group
}

func TestFeatureVectorValidationAndImmutability(t *testing.T) {
	source := []float64{1, 2}
	vector, err := NewFeatureVector(source)
	if err != nil {
		t.Fatalf("NewFeatureVector: %v", err)
	}
	source[0] = 9
	returned := vector.Values()
	returned[1] = 9
	if vector.Dimension() != 2 || !reflect.DeepEqual(vector.Values(), []float64{1, 2}) {
		t.Fatalf("vector changed through caller slices: %#v", vector.Values())
	}
	known := []bool{true, false}
	sparse, err := NewFeatureVectorWithKnownValues([]float64{3, 0}, known)
	if err != nil {
		t.Fatalf("NewFeatureVectorWithKnownValues: %v", err)
	}
	known[0] = false
	if !sparse.Known(0) || sparse.Known(1) || sparse.Known(-1) || sparse.Known(2) {
		t.Fatalf("sparse feature presence changed")
	}
	if _, err := NewFeatureVectorWithKnownValues([]float64{1}, nil); err == nil {
		t.Fatalf("mismatched feature presence was accepted")
	}

	invalid := [][]float64{
		nil,
		make([]float64, maximumLinearFeatures+1),
		{math.NaN()},
		{math.Inf(1)},
		{maximumLinearFeatureMagnitude + 1},
	}
	for _, values := range invalid {
		if _, err := NewFeatureVector(values); err == nil {
			t.Errorf("NewFeatureVector(%v) succeeded", values)
		}
	}
}

func TestRankingExampleValidationAndImmutability(t *testing.T) {
	vector := mustLinearFeatureVector(t, 1, 2)
	example, err := NewRankingExample("document", 3, vector)
	if err != nil {
		t.Fatalf("NewRankingExample: %v", err)
	}
	copyOfFeatures := example.Features()
	copyOfFeatures.values[0] = 9
	copyOfFeatures.known[0] = false
	if example.DocumentIdentifier() != "document" || example.Relevance() != 3 {
		t.Fatalf("example accessors returned unexpected values")
	}
	if !reflect.DeepEqual(example.Features().Values(), []float64{1, 2}) ||
		!example.Features().Known(0) {
		t.Fatalf("example features changed through accessor")
	}

	cases := []struct {
		identifier string
		relevance  int
		features   FeatureVector
	}{
		{"", 0, vector},
		{"negative", -1, vector},
		{"large", maximumRelevanceGrade + 1, vector},
		{"empty-features", 0, FeatureVector{}},
	}
	for _, testCase := range cases {
		if _, err := NewRankingExample(
			testCase.identifier,
			testCase.relevance,
			testCase.features,
		); err == nil {
			t.Errorf("NewRankingExample(%q) succeeded", testCase.identifier)
		}
	}
}

func TestQueryGroupValidationAndImmutability(t *testing.T) {
	first := mustRankingExample(t, "first", 2, 1, 2)
	second := mustRankingExample(t, "second", 1, 3, 4)
	input := []RankingExample{first, second}
	group, err := NewQueryGroup("query", input)
	if err != nil {
		t.Fatalf("NewQueryGroup: %v", err)
	}
	input[0].documentIdentifier = "changed"
	returned := group.Examples()
	returned[0].documentIdentifier = "also-changed"
	returned[1].features.values[0] = 99
	if group.QueryIdentifier() != "query" {
		t.Fatalf("QueryIdentifier = %q", group.QueryIdentifier())
	}
	if got := group.Examples(); got[0].DocumentIdentifier() != "first" ||
		got[1].Features().Values()[0] != 3 {
		t.Fatalf("group changed through caller data: %#v", got)
	}

	tooMany := make([]RankingExample, maximumLinearExamplesPerQuery+1)
	invalidExample := RankingExample{}
	differentDimension := mustRankingExample(t, "different", 0, 1)
	cases := []struct {
		identifier string
		examples   []RankingExample
	}{
		{"", []RankingExample{first}},
		{"empty", nil},
		{"large", tooMany},
		{"invalid", []RankingExample{invalidExample}},
		{"dimension", []RankingExample{first, differentDimension}},
		{"duplicate", []RankingExample{first, first}},
	}
	for _, testCase := range cases {
		if _, err := NewQueryGroup(testCase.identifier, testCase.examples); err == nil {
			t.Errorf("NewQueryGroup(%q) succeeded", testCase.identifier)
		}
	}
}

func TestFeatureDefinitionValidation(t *testing.T) {
	valid := []FeatureDefinition{
		{Name: "free", Direction: FeatureUnconstrained},
		{Name: "up", Direction: FeatureIncreasing},
		{Name: "down", Direction: FeatureDecreasing},
	}
	if err := validateFeatureDefinitions(valid); err != nil {
		t.Fatalf("validateFeatureDefinitions: %v", err)
	}

	cases := [][]FeatureDefinition{
		nil,
		make([]FeatureDefinition, maximumLinearFeatures+1),
		{{Name: "", Direction: FeatureUnconstrained}},
		{{Name: "invalid name", Direction: FeatureUnconstrained}},
		{{Name: "1invalid", Direction: FeatureUnconstrained}},
		{{Name: "bad", Direction: FeatureDirection(-1)}},
		{{Name: "same"}, {Name: "same"}},
	}
	for _, definitions := range cases {
		if err := validateFeatureDefinitions(definitions); err == nil {
			t.Errorf("validateFeatureDefinitions(%v) succeeded", definitions)
		}
	}
}

func TestLinearModelPredictionExplanationAndImmutability(t *testing.T) {
	definitions := []FeatureDefinition{
		{Name: "relevance", Direction: FeatureIncreasing},
		{Name: "risk", Direction: FeatureDecreasing},
	}
	weights := []float64{2, -1}
	model, err := NewLinearLambdaRankModel(definitions, weights)
	if err != nil {
		t.Fatalf("NewLinearLambdaRankModel: %v", err)
	}
	definitions[0].Name = "changed"
	weights[0] = 99
	returnedDefinitions := model.FeatureDefinitions()
	returnedDefinitions[0].Name = "also-changed"
	returnedWeights := model.Weights()
	returnedWeights[0] = 99
	if err := model.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if model.FeatureDefinitions()[0].Name != "relevance" || model.Weights()[0] != 2 {
		t.Fatalf("model changed through caller data")
	}

	group := mustQueryGroup(
		t,
		"query",
		mustRankingExample(t, "z-document", 0, 2, 0),
		mustRankingExample(t, "b-document", 0, 0, 2),
		mustRankingExample(t, "a-document", 0, 1, 1),
	)
	predictions, err := model.Predict(group)
	if err != nil {
		t.Fatalf("Predict: %v", err)
	}
	if got := []string{
		predictions[0].DocumentIdentifier,
		predictions[1].DocumentIdentifier,
		predictions[2].DocumentIdentifier,
	}; !reflect.DeepEqual(got, []string{"z-document", "a-document", "b-document"}) {
		t.Fatalf("prediction order = %v", got)
	}
	for index, prediction := range predictions {
		if prediction.Rank != index+1 {
			t.Errorf("prediction rank = %d, want %d", prediction.Rank, index+1)
		}
	}

	explanations, err := model.Explain(group)
	if err != nil {
		t.Fatalf("Explain: %v", err)
	}
	assertLinearExplanations(t, explanations)

	tieModel, err := NewLinearLambdaRankModel(definitionsForTest("zero"), []float64{0})
	if err != nil {
		t.Fatalf("NewLinearLambdaRankModel tie: %v", err)
	}
	tieGroup := mustQueryGroup(
		t,
		"tie",
		mustRankingExample(t, "b", 0, 1),
		mustRankingExample(t, "a", 0, 2),
	)
	ties, err := tieModel.Predict(tieGroup)
	if err != nil || ties[0].DocumentIdentifier != "b" {
		t.Fatalf("tie prediction = %#v, err = %v", ties, err)
	}
}

func assertLinearExplanations(t *testing.T, explanations []RankingExplanation) {
	t.Helper()
	for index, explanation := range explanations {
		total := 0.0
		for _, contribution := range explanation.FeatureContributions {
			total += contribution.Contribution
			if contribution.FeatureName == "" || contribution.Weight == 0 ||
				!contribution.Known || !contribution.Used {
				t.Errorf("incomplete contribution: %#v", contribution)
			}
		}
		if explanation.Rank != index+1 || math.Abs(total-explanation.Score) > 1e-12 {
			t.Errorf("explanation = %#v, contribution total = %v", explanation, total)
		}
	}
}

func TestLinearModelValidationFailures(t *testing.T) {
	validDefinitions := definitionsForTest("feature")
	if _, err := newLinearLambdaRankModel(validDefinitions, []float64{1}, 0); err == nil {
		t.Errorf("newLinearLambdaRankModel accepted an invalid missing policy")
	}
	cases := []struct {
		definitions []FeatureDefinition
		weights     []float64
	}{
		{nil, nil},
		{validDefinitions, nil},
		{validDefinitions, []float64{math.NaN()}},
		{validDefinitions, []float64{math.Inf(-1)}},
		{validDefinitions, []float64{maximumLinearWeightMagnitude + 1}},
		{[]FeatureDefinition{{Name: "up", Direction: FeatureIncreasing}}, []float64{-1}},
		{[]FeatureDefinition{{Name: "down", Direction: FeatureDecreasing}}, []float64{1}},
	}
	for _, testCase := range cases {
		if _, err := NewLinearLambdaRankModel(testCase.definitions, testCase.weights); err == nil {
			t.Errorf(
				"NewLinearLambdaRankModel(%v, %v) succeeded",
				testCase.definitions,
				testCase.weights,
			)
		}
	}

	validModel, err := NewLinearLambdaRankModel(validDefinitions, []float64{1})
	if err != nil {
		t.Fatalf("NewLinearLambdaRankModel: %v", err)
	}
	if _, err := validModel.Predict(QueryGroup{}); err == nil {
		t.Errorf("Predict accepted an invalid group")
	}
	if _, err := (LinearLambdaRankModel{}).Predict(mustQueryGroup(
		t,
		"query",
		mustRankingExample(t, "document", 0, 1),
	)); err == nil {
		t.Errorf("Predict accepted an invalid model")
	}
	if _, err := validModel.Explain(QueryGroup{}); err == nil {
		t.Errorf("Explain accepted an invalid group")
	}
	wrongDimension := mustQueryGroup(
		t,
		"wrong",
		mustRankingExample(t, "document", 0, 1, 2),
	)
	if _, err := validModel.Predict(wrongDimension); err == nil {
		t.Errorf("Predict accepted a dimension mismatch")
	}
	if compareIdentifiers("a", "b") != -1 || compareIdentifiers("b", "a") != 1 ||
		compareIdentifiers("a", "a") != 0 {
		t.Errorf("compareIdentifiers ordering is incorrect")
	}
}

func TestLinearModelJSONRoundTripAndRejection(t *testing.T) {
	model, err := NewLinearLambdaRankModel(
		[]FeatureDefinition{
			{Name: "quality", Direction: FeatureIncreasing},
			{Name: "risk", Direction: FeatureDecreasing},
		},
		[]float64{1.25, -0.5},
	)
	if err != nil {
		t.Fatalf("NewLinearLambdaRankModel: %v", err)
	}
	encoded, err := json.Marshal(model)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded LinearLambdaRankModel
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !reflect.DeepEqual(decoded.FeatureDefinitions(), model.FeatureDefinitions()) ||
		!reflect.DeepEqual(decoded.Weights(), model.Weights()) {
		t.Fatalf("decoded model differs: %#v", decoded)
	}

	if _, err := json.Marshal(LinearLambdaRankModel{}); err == nil {
		t.Errorf("Marshal accepted an invalid model")
	}
	if err := decoded.UnmarshalJSON([]byte("{")); err == nil {
		t.Errorf("UnmarshalJSON accepted malformed JSON")
	}
	invalidDocuments := []string{
		`{"format":"other","features":[{"name":"x","direction":0}],"weights":[1]}`,
		`{"format":"yago-linear-lambdarank-v1","features":[],"weights":[]}`,
	}
	for _, document := range invalidDocuments {
		before := decoded.Weights()
		if err := json.Unmarshal([]byte(document), &decoded); err == nil {
			t.Errorf("Unmarshal(%q) succeeded", document)
		}
		if !reflect.DeepEqual(decoded.Weights(), before) {
			t.Errorf("failed Unmarshal changed the receiver")
		}
	}
	if !strings.Contains(string(encoded), linearLambdaRankFormat) {
		t.Errorf("encoded model lacks format: %s", encoded)
	}
}

func TestLinearModelFormatsPreserveMissingEvidenceSemantics(t *testing.T) {
	var legacy LinearLambdaRankModel
	if err := json.Unmarshal([]byte(
		`{"format":"yago-linear-lambdarank-v1","features":[{"name":"quality","direction":0}],"weights":[1]}`,
	), &legacy); err != nil {
		t.Fatalf("decode legacy model: %v", err)
	}
	neutral, err := NewLinearLambdaRankModel(definitionsForTest("quality"), []float64{1})
	if err != nil {
		t.Fatalf("new neutral model: %v", err)
	}
	group := mustQueryGroup(
		t,
		"missing",
		mustKnownRankingExample(t, "low", []float64{1}, []bool{true}),
		mustKnownRankingExample(t, "missing", []float64{0}, []bool{false}),
		mustKnownRankingExample(t, "high", []float64{3}, []bool{true}),
	)
	legacyRanked, err := legacy.Predict(group)
	if err != nil {
		t.Fatalf("legacy prediction: %v", err)
	}
	neutralRanked, err := neutral.Predict(group)
	if err != nil {
		t.Fatalf("neutral prediction: %v", err)
	}
	if got := []string{
		legacyRanked[0].DocumentIdentifier,
		legacyRanked[1].DocumentIdentifier,
		legacyRanked[2].DocumentIdentifier,
	}; !reflect.DeepEqual(got, []string{"high", "low", "missing"}) {
		t.Fatalf("legacy order = %v", got)
	}
	if got := []string{
		neutralRanked[0].DocumentIdentifier,
		neutralRanked[1].DocumentIdentifier,
		neutralRanked[2].DocumentIdentifier,
	}; !reflect.DeepEqual(got, []string{"high", "missing", "low"}) {
		t.Fatalf("neutral order = %v", got)
	}
	assertMissingLinearContribution(t, legacy, group, true)
	assertMissingLinearContribution(t, neutral, group, false)
	encodedLegacy, err := json.Marshal(legacy)
	if err != nil || !strings.Contains(string(encodedLegacy), linearLambdaRankLegacyFormat) {
		t.Fatalf("legacy encoding = %s, %v", encodedLegacy, err)
	}
}

func assertMissingLinearContribution(
	t *testing.T,
	model LinearLambdaRankModel,
	group QueryGroup,
	legacy bool,
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
		contribution := explanation.FeatureContributions[0]
		if contribution.Known || contribution.Used != legacy ||
			(contribution.Contribution != 0) != legacy {
			t.Fatalf("missing contribution = %#v", contribution)
		}

		return
	}
	t.Fatal("missing contribution was not found")
}

func definitionsForTest(names ...string) []FeatureDefinition {
	definitions := make([]FeatureDefinition, len(names))
	for index, name := range names {
		definitions[index] = FeatureDefinition{Name: name}
	}

	return definitions
}
