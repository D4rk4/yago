package searcheval

import (
	"encoding/json"
	"math"
	"reflect"
	"testing"
)

func TestPlattCalibrationFitsMonotoneProbabilitiesAndJSON(t *testing.T) {
	samples := []CalibrationSample{
		{Score: -3},
		{Score: -2},
		{Score: -1},
		{Score: 1, Relevant: true},
		{Score: 2, Relevant: true},
		{Score: 3, Relevant: true},
	}
	model, err := FitPlattCalibration(samples)
	if err != nil {
		t.Fatalf("FitPlattCalibration: %v", err)
	}
	if model.Slope <= 0 || model.Probability(-2) >= model.Probability(2) ||
		model.Probability(math.Inf(-1)) != 0 || model.Probability(math.Inf(1)) != 1 ||
		model.Probability(math.NaN()) != 0.5 {
		t.Fatalf(
			"model = %+v probabilities=%v %v",
			model,
			model.Probability(-2),
			model.Probability(2),
		)
	}
	data, err := json.Marshal(model)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	decoded, err := ParsePlattModelJSON(data)
	if err != nil || decoded != model {
		t.Fatalf("roundtrip = %+v err=%v", decoded, err)
	}
	var direct PlattModel
	if err := json.Unmarshal(data, &direct); err != nil || direct != model {
		t.Fatalf("direct roundtrip = %+v err=%v", direct, err)
	}
}

func TestPlattCalibrationConstantAndInvertedScoresStayMonotone(t *testing.T) {
	constant, err := FitPlattCalibration([]CalibrationSample{
		{Score: 2}, {Score: 2, Relevant: true},
	})
	if err != nil || constant.Slope != 0 ||
		constant.Probability(-100) != constant.Probability(100) {
		t.Fatalf("constant = %+v err=%v", constant, err)
	}
	inverted, err := FitPlattCalibration([]CalibrationSample{
		{Score: -2, Relevant: true},
		{Score: -1, Relevant: true},
		{Score: 1},
		{Score: 2},
	})
	if err != nil || inverted.Slope < 0 || inverted.Probability(-2) > inverted.Probability(2) {
		t.Fatalf("inverted = %+v err=%v", inverted, err)
	}
	invalid := PlattModel{Slope: -1, Intercept: 2}
	if invalid.Probability(-10) != invalid.Probability(10) {
		t.Fatal("invalid negative slope inverted ranking")
	}
}

func TestPlattCalibrationRejectsInvalidState(t *testing.T) {
	invalidSamples := [][]CalibrationSample{
		nil,
		{{Score: math.NaN()}, {Score: 1, Relevant: true}},
		{{Score: math.Inf(1)}, {Score: 1, Relevant: true}},
		{{Score: 0}, {Score: 1}},
		{{Score: 0, Relevant: true}, {Score: 1, Relevant: true}},
	}
	for _, samples := range invalidSamples {
		if _, err := FitPlattCalibration(samples); err == nil {
			t.Fatalf("samples accepted: %+v", samples)
		}
	}
	models := []PlattModel{
		{Slope: -1},
		{Slope: math.NaN()},
		{Slope: math.Inf(1)},
		{Intercept: math.NaN()},
		{Intercept: math.Inf(1)},
	}
	for _, model := range models {
		if model.Validate() == nil {
			t.Fatalf("model accepted: %+v", model)
		}
		if _, err := json.Marshal(model); err == nil {
			t.Fatalf("invalid model marshaled: %+v", model)
		}
	}
	for _, data := range [][]byte{
		[]byte(`{"slope":-1,"intercept":0}`),
		[]byte(`{"slope":`),
	} {
		if _, err := ParsePlattModelJSON(data); err == nil {
			t.Fatalf("invalid JSON accepted: %s", data)
		}
	}
	var direct PlattModel
	if err := direct.UnmarshalJSON([]byte(`{"slope":`)); err == nil {
		t.Fatal("malformed direct Platt JSON accepted")
	}
}

func TestIsotonicCalibrationPAVMonotoneAndJSON(t *testing.T) {
	samples := []CalibrationSample{
		{Score: 0},
		{Score: 1, Relevant: true},
		{Score: 2},
		{Score: 2, Relevant: true},
		{Score: 3, Relevant: true},
	}
	model, err := FitIsotonicCalibration(samples)
	if err != nil {
		t.Fatalf("FitIsotonicCalibration: %v", err)
	}
	if len(model.Thresholds) >= len(samples) || model.Probability(math.NaN()) != 0.5 ||
		model.Probability(-1) > model.Probability(1) ||
		model.Probability(1) > model.Probability(2) ||
		model.Probability(math.Inf(1)) != model.Probabilities[len(model.Probabilities)-1] {
		t.Fatalf("model = %+v", model)
	}
	data, err := json.Marshal(model)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	decoded, err := ParseIsotonicModelJSON(data)
	if err != nil || !reflect.DeepEqual(decoded, model) {
		t.Fatalf("roundtrip = %+v err=%v", decoded, err)
	}
	var direct IsotonicModel
	if err := json.Unmarshal(data, &direct); err != nil || !reflect.DeepEqual(direct, model) {
		t.Fatalf("direct roundtrip = %+v err=%v", direct, err)
	}
	decoded.Thresholds[0] = 99
	if direct.Thresholds[0] == 99 {
		t.Fatal("decoded models share threshold storage")
	}
	allPositive, err := FitIsotonicCalibration([]CalibrationSample{
		{Score: 1, Relevant: true}, {Score: 1, Relevant: true},
	})
	if err != nil || len(allPositive.Thresholds) != 1 || allPositive.Probability(1) != 1 {
		t.Fatalf("all-positive = %+v err=%v", allPositive, err)
	}
}

func TestIsotonicCalibrationRejectsInvalidState(t *testing.T) {
	if _, err := FitIsotonicCalibration(nil); err == nil {
		t.Fatal("empty samples accepted")
	}
	if _, err := FitIsotonicCalibration([]CalibrationSample{{Score: math.NaN()}}); err == nil {
		t.Fatal("non-finite sample accepted")
	}
	models := []IsotonicModel{
		{},
		{Thresholds: []float64{1}, Probabilities: []float64{}},
		{Thresholds: []float64{math.NaN()}, Probabilities: []float64{0}},
		{Thresholds: []float64{math.Inf(1)}, Probabilities: []float64{0}},
		{Thresholds: []float64{1, 1}, Probabilities: []float64{0, 1}},
		{Thresholds: []float64{1}, Probabilities: []float64{math.NaN()}},
		{Thresholds: []float64{1}, Probabilities: []float64{math.Inf(1)}},
		{Thresholds: []float64{1}, Probabilities: []float64{-1}},
		{Thresholds: []float64{1}, Probabilities: []float64{2}},
		{Thresholds: []float64{1, 2}, Probabilities: []float64{1, 0}},
	}
	for _, model := range models {
		if model.Validate() == nil {
			t.Fatalf("model accepted: %+v", model)
		}
		if _, err := json.Marshal(model); err == nil {
			t.Fatalf("invalid model marshaled: %+v", model)
		}
	}
	for _, data := range [][]byte{
		[]byte(`{"thresholds":[1,1],"probabilities":[0,1]}`),
		[]byte(`{"thresholds":`),
	} {
		if _, err := ParseIsotonicModelJSON(data); err == nil {
			t.Fatalf("invalid JSON accepted: %s", data)
		}
	}
	var direct IsotonicModel
	if err := direct.UnmarshalJSON([]byte(`{"thresholds":`)); err == nil {
		t.Fatal("malformed direct isotonic JSON accepted")
	}
}

func TestCalibrationMappingsNeverInvertRanking(t *testing.T) {
	samples := []CalibrationSample{
		{Score: -2},
		{Score: -1},
		{Score: 0},
		{Score: 1, Relevant: true},
		{Score: 2, Relevant: true},
	}
	platt, err := FitPlattCalibration(samples)
	if err != nil {
		t.Fatalf("FitPlattCalibration: %v", err)
	}
	isotonic, err := FitIsotonicCalibration(samples)
	if err != nil {
		t.Fatalf("FitIsotonicCalibration: %v", err)
	}
	for first := -20; first <= 20; first++ {
		for second := first; second <= 20; second++ {
			if platt.Probability(float64(first)) > platt.Probability(float64(second)) ||
				isotonic.Probability(float64(first)) > isotonic.Probability(float64(second)) {
				t.Fatalf("ranking inverted at %d %d", first, second)
			}
		}
	}
}
