package contentsafety

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type stagedCancellationContext struct {
	checks atomic.Int64
	failAt int64
}

func (c *stagedCancellationContext) Deadline() (time.Time, bool) {
	return time.Time{}, false
}

func (c *stagedCancellationContext) Done() <-chan struct{} {
	return nil
}

func (c *stagedCancellationContext) Err() error {
	if c.checks.Add(1) >= c.failAt {
		return context.Canceled
	}

	return nil
}

func (c *stagedCancellationContext) Value(any) any {
	return nil
}

func TestTrainCharacterModelLearnsDeterministically(t *testing.T) {
	documents := trainingCorpus()
	model := trainFixtureModel(t, documents)
	assertExpectedRating(
		t,
		model.Classify("family archive catalogue delta calm public"),
		General,
	)
	assertExpectedRating(
		t,
		model.Classify("restricted mature section lambda private"),
		Explicit,
	)
	reversed := append([]LabeledDocument(nil), documents...)
	for left, right := 0, len(reversed)-1; left < right; left, right = left+1, right-1 {
		reversed[left], reversed[right] = reversed[right], reversed[left]
	}
	reorderedModel := trainFixtureModel(t, reversed)
	encoded, err := json.Marshal(model)
	if err != nil {
		t.Fatalf("Marshal model: %v", err)
	}
	reorderedEncoded, err := json.Marshal(reorderedModel)
	if err != nil {
		t.Fatalf("Marshal reordered model: %v", err)
	}
	if string(encoded) != string(reorderedEncoded) {
		t.Fatal("training depends on caller document order")
	}
}

func TestTrainCharacterModelSupportsMultilingualText(t *testing.T) {
	model := trainFixtureModel(t, []LabeledDocument{
		{Text: "一般向け資料 東京 公共 案内 甲", Rating: General},
		{Text: "一般向け資料 東京 公共 案内 乙", Rating: General},
		{Text: "一般向け資料 東京 公共 案内 丙", Rating: General},
		{Text: "محتوى مقيّد قسم خاص ألف", Rating: Explicit},
		{Text: "محتوى مقيّد قسم خاص باء", Rating: Explicit},
		{Text: "محتوى مقيّد قسم خاص جيم", Rating: Explicit},
	})
	assertExpectedRating(t, model.Classify("一般向け資料 東京 公共 案内 丁"), General)
	assertExpectedRating(t, model.Classify("محتوى مقيّد قسم خاص دال"), Explicit)
}

func TestTrainCharacterModelRejectsInvalidInput(t *testing.T) {
	valid := trainingCorpus()
	if _, err := TrainCharacterModel(nilContextForTest(), valid); err == nil {
		t.Fatal("nil context succeeded")
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := TrainCharacterModel(canceled, valid); err == nil {
		t.Fatal("canceled context succeeded")
	}
	invalidCorpora := [][]LabeledDocument{
		nil,
		append(valid, make([]LabeledDocument, MaximumLabeledDocuments-len(valid)+1)...),
		{
			{Text: "general alpha", Rating: General},
			{Text: "general beta", Rating: General},
			{Text: "general gamma", Rating: General},
			{Text: "explicit alpha", Rating: Explicit},
		},
		{
			{Text: "general alpha", Rating: General},
			{Text: "general beta", Rating: General},
			{Text: "explicit alpha", Rating: Explicit},
			{Text: "", Rating: Explicit},
		},
		{
			{Text: "general alpha", Rating: General},
			{Text: "general beta", Rating: General},
			{Text: "explicit alpha", Rating: Explicit},
			{Text: strings.Repeat("界", MaximumLabeledDocumentRunes+1), Rating: Explicit},
		},
		{
			{Text: "general alpha", Rating: General},
			{Text: "general beta", Rating: General},
			{Text: "explicit alpha", Rating: Explicit},
			{Text: "unknown alpha", Rating: Unknown},
		},
		{
			{Text: "a", Rating: General},
			{Text: "b", Rating: General},
			{Text: "c", Rating: Explicit},
			{Text: "d", Rating: Explicit},
		},
	}
	for index, documents := range invalidCorpora {
		if _, err := TrainCharacterModel(context.Background(), documents); err == nil {
			t.Fatalf("invalid corpus %d succeeded", index)
		}
	}
}

func nilContextForTest() context.Context {
	return nil
}

func TestTrainCharacterModelPropagatesPhaseCancellation(t *testing.T) {
	documents := trainingCorpus()
	phaseCorpus := []LabeledDocument{documents[0], documents[1], documents[3], documents[4]}
	if _, err := TrainCharacterModel(
		&stagedCancellationContext{failAt: 6},
		phaseCorpus,
	); err == nil {
		t.Fatal("logistic phase cancellation succeeded")
	}
	if _, err := TrainCharacterModel(
		&stagedCancellationContext{failAt: 86},
		phaseCorpus,
	); err == nil {
		t.Fatal("calibration phase cancellation succeeded")
	}
}

func TestTrainingPrimitives(t *testing.T) {
	ordered, err := validateLabeledDocuments(trainingCorpus())
	if err != nil {
		t.Fatalf("validateLabeledDocuments: %v", err)
	}
	if compareLabeledDocuments(ordered[0], ordered[0]) != 0 ||
		compareLabeledDocuments(ordered[1], ordered[0]) <= 0 ||
		compareLabeledDocuments(ordered[len(ordered)-1], ordered[0]) <= 0 ||
		compareLabeledDocuments(ordered[0], ordered[len(ordered)-1]) >= 0 {
		t.Fatal("labeled document ordering is invalid")
	}
	prepared, err := prepareDocuments(context.Background(), ordered)
	if err != nil {
		t.Fatalf("prepareDocuments: %v", err)
	}
	training, calibration := splitPreparedDocuments(prepared)
	if len(training)+len(calibration) != len(prepared) || len(calibration) != 2 {
		t.Fatalf("split sizes = %d training, %d calibration", len(training), len(calibration))
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := prepareDocuments(canceled, ordered); err == nil {
		t.Fatal("canceled preparation succeeded")
	}
	if _, _, err := fitCharacterLogistic(canceled, training); err == nil {
		t.Fatal("canceled logistic fitting succeeded")
	}
	weights := make([]float64, CharacterFeatureSpaceSize)
	if _, _, err := fitCharacterCalibration(canceled, calibration, weights, 0); err == nil {
		t.Fatal("canceled calibration succeeded")
	}
	if got := boundedUpdate(maximumCoefficientMagnitude, 1); got != maximumCoefficientMagnitude {
		t.Fatalf("positive bounded update = %v", got)
	}
	if got := boundedUpdate(-maximumCoefficientMagnitude, -1); got != -maximumCoefficientMagnitude {
		t.Fatalf("negative bounded update = %v", got)
	}
}

func trainFixtureModel(t *testing.T, documents []LabeledDocument) CharacterModel {
	t.Helper()
	model, err := TrainCharacterModel(context.Background(), documents)
	if err != nil {
		t.Fatalf("TrainCharacterModel: %v", err)
	}
	if err := model.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	return model
}

func trainingCorpus() []LabeledDocument {
	return []LabeledDocument{
		{Text: "family archive catalogue alpha calm public", Rating: General},
		{Text: "family archive catalogue beta calm public", Rating: General},
		{Text: "family archive catalogue gamma calm public", Rating: General},
		{Text: "restricted mature section omega private", Rating: Explicit},
		{Text: "restricted mature section sigma private", Rating: Explicit},
		{Text: "restricted mature section theta private", Rating: Explicit},
	}
}
