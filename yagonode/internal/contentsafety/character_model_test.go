package contentsafety

import (
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestCharacterModelJSONRoundTrip(t *testing.T) {
	model := trainFixtureModel(t, trainingCorpus())
	encoded, err := json.Marshal(model)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded CharacterModel
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	reencoded, err := json.Marshal(decoded)
	if err != nil {
		t.Fatalf("Marshal decoded: %v", err)
	}
	if !reflect.DeepEqual(encoded, reencoded) {
		t.Fatal("model JSON round trip changed bytes")
	}
	if got, want := decoded.Classify("family archive catalogue delta calm"),
		model.Classify("family archive catalogue delta calm"); got != want {
		t.Fatalf("decoded classification = %#v, want %#v", got, want)
	}
}

func TestCharacterModelJSONRejectsInvalidDocumentsWithoutMutation(t *testing.T) {
	model := trainFixtureModel(t, trainingCorpus())
	before, err := json.Marshal(model)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	validDocument := characterModelDocument{
		Format:               characterModelFormat,
		FeatureSpaceSize:     CharacterFeatureSpaceSize,
		MinimumGramLength:    minimumCharacterGramLength,
		MaximumGramLength:    maximumCharacterGramLength,
		Weights:              make([]float64, CharacterFeatureSpaceSize),
		CalibrationSlope:     1,
		CalibrationIntercept: 0,
	}
	wrongShape := validDocument
	wrongShape.FeatureSpaceSize++
	wrongWeights := validDocument
	wrongWeights.Weights = []float64{1}
	wrongFormat := validDocument
	wrongFormat.Format = "other"
	invalidDocuments := [][]byte{
		mustEncodeModelDocument(t, wrongShape),
		mustEncodeModelDocument(t, wrongWeights),
		mustEncodeModelDocument(t, wrongFormat),
	}
	if err := model.UnmarshalJSON([]byte("{")); err == nil {
		t.Fatal("direct malformed model decode succeeded")
	}
	for _, encoded := range invalidDocuments {
		if err := json.Unmarshal(encoded, &model); err == nil {
			t.Fatalf("Unmarshal(%q) succeeded", encoded)
		}
		after, marshalErr := json.Marshal(model)
		if marshalErr != nil {
			t.Fatalf("Marshal after rejection: %v", marshalErr)
		}
		if !reflect.DeepEqual(before, after) {
			t.Fatal("rejected model document mutated receiver")
		}
	}
	if _, err := json.Marshal(CharacterModel{}); err == nil {
		t.Fatal("zero model marshaled")
	}
}

func TestCharacterModelValidation(t *testing.T) {
	valid := CharacterModel{
		weights:          make([]float64, CharacterFeatureSpaceSize),
		calibrationSlope: 1,
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid model: %v", err)
	}
	invalidModels := []CharacterModel{
		{},
		modelWithWeight(math.NaN()),
		modelWithWeight(maximumCoefficientMagnitude + 1),
		modelWithParameters(math.Inf(1), 1, 0),
		modelWithParameters(0, 1, math.Inf(-1)),
		modelWithParameters(0, 0, 0),
		modelWithParameters(0, math.Inf(1), 0),
	}
	for _, model := range invalidModels {
		if err := model.Validate(); err == nil {
			t.Fatalf("invalid model validated: %#v", model)
		}
	}
	if got := (CharacterModel{}).Classify("document text"); got.Rating != Unknown {
		t.Fatalf("zero-model classification = %#v", got)
	}
	if got := valid.Classify("ab"); got.Rating != Unknown {
		t.Fatalf("featureless classification = %#v", got)
	}
}

func TestCharacterModelLogisticBounds(t *testing.T) {
	if got := logistic(1000); got != 1 {
		t.Fatalf("logistic positive saturation = %v", got)
	}
	if got := logistic(-1000); got != 0 {
		t.Fatalf("logistic negative saturation = %v", got)
	}
}

func TestCharacterModelConcurrentRead(t *testing.T) {
	model := trainFixtureModel(t, trainingCorpus())
	var wait sync.WaitGroup
	for range 16 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for range 25 {
				evidence := model.Classify("restricted mature section lambda")
				if evidence.ExplicitProbability < 0 || evidence.ExplicitProbability > 1 {
					t.Errorf("probability out of range: %#v", evidence)
				}
				if _, err := json.Marshal(model); err != nil {
					t.Errorf("Marshal: %v", err)
				}
			}
		}()
	}
	wait.Wait()
}

func mustEncodeModelDocument(t *testing.T, document characterModelDocument) []byte {
	t.Helper()
	encoded, err := json.Marshal(document)
	if err != nil {
		t.Fatalf("Marshal model document: %v", err)
	}

	return encoded
}

func modelWithWeight(weight float64) CharacterModel {
	model := modelWithParameters(0, 1, 0)
	model.weights[0] = weight

	return model
}

func modelWithParameters(intercept, slope, calibrationIntercept float64) CharacterModel {
	return CharacterModel{
		weights:              make([]float64, CharacterFeatureSpaceSize),
		intercept:            intercept,
		calibrationSlope:     slope,
		calibrationIntercept: calibrationIntercept,
	}
}

func assertExpectedRating(t *testing.T, evidence Evidence, rating Rating) {
	t.Helper()
	if evidence.Rating != rating {
		t.Fatalf("rating = %v, want %v; evidence=%#v", evidence.Rating, rating, evidence)
	}
	if math.IsNaN(evidence.ExplicitProbability) || math.IsNaN(evidence.Confidence) ||
		evidence.ExplicitProbability < 0 || evidence.ExplicitProbability > 1 ||
		evidence.Confidence < 0 || evidence.Confidence > 1 {
		t.Fatalf("invalid evidence = %#v", evidence)
	}
}

func TestCharacterModelInputIsBounded(t *testing.T) {
	model := trainFixtureModel(t, trainingCorpus())
	prefix := strings.Repeat("restricted mature section ", 400)
	oversize := prefix + strings.Repeat("family archive catalogue ", 400)
	if got, want := model.Classify(oversize), model.Classify(prefix); got != want {
		t.Fatalf("oversize input was not deterministically bounded: %#v != %#v", got, want)
	}
}
