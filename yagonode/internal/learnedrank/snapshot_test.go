package learnedrank

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/rankfit"
)

func TestLinearAndHistogramSnapshotsExposeImmutableModelValues(t *testing.T) {
	linear := mustLinearModel(t, linearWeights(map[int]float64{0: 1, 2: -1}))
	linearSnapshot, err := NewLinearSnapshot("linear-v1", linear)
	if err != nil {
		t.Fatalf("NewLinearSnapshot: %v", err)
	}
	if linearSnapshot.Revision() != "linear-v1" ||
		linearSnapshot.Kind() != ModelLinearLambdaRank {
		t.Fatalf("linear metadata = %q, %q", linearSnapshot.Revision(), linearSnapshot.Kind())
	}
	returnedLinear, ok := linearSnapshot.LinearModel()
	if !ok || !reflect.DeepEqual(returnedLinear.Weights(), linear.Weights()) {
		t.Fatalf("linear accessor = %#v, %v", returnedLinear, ok)
	}
	if _, ok := linearSnapshot.HistogramModel(); ok {
		t.Fatalf("linear snapshot exposed a histogram model")
	}

	histogram := mustHistogramModel(t)
	histogramSnapshot, err := NewHistogramSnapshot("histogram-v1", histogram)
	if err != nil {
		t.Fatalf("NewHistogramSnapshot: %v", err)
	}
	returnedHistogram, ok := histogramSnapshot.HistogramModel()
	if !ok || returnedHistogram.TreeCount() != histogram.TreeCount() {
		t.Fatalf("histogram accessor = %#v, %v", returnedHistogram, ok)
	}
	if _, ok := histogramSnapshot.LinearModel(); ok {
		t.Fatalf("histogram snapshot exposed a linear model")
	}
}

func TestSnapshotValidationRejectsInvalidRevisionStateAndCatalog(t *testing.T) {
	linear := mustLinearModel(t, linearWeights(nil))
	histogram := mustHistogramModel(t)
	wrongLinear, err := rankfit.NewLinearLambdaRankModel(
		[]rankfit.FeatureDefinition{{Name: "other"}},
		[]float64{0},
	)
	if err != nil {
		t.Fatalf("wrong linear model: %v", err)
	}
	wrongHistogram := mustWrongHistogramModel(t)

	invalid := []Snapshot{
		{},
		{revision: "v1", kind: ModelLinearLambdaRank},
		{revision: "v1", kind: ModelLinearLambdaRank, linear: &linear, histogram: &histogram},
		{revision: "v1", kind: ModelLinearLambdaRank, linear: &rankfit.LinearLambdaRankModel{}},
		{revision: "v1", kind: ModelLinearLambdaRank, linear: &wrongLinear},
		{revision: "v1", kind: ModelHistogramLambdaMART},
		{revision: "v1", kind: ModelHistogramLambdaMART, linear: &linear, histogram: &histogram},
		{
			revision:  "v1",
			kind:      ModelHistogramLambdaMART,
			histogram: &rankfit.HistogramLambdaMARTModel{},
		},
		{revision: "v1", kind: ModelHistogramLambdaMART, histogram: &wrongHistogram},
		{revision: "v1", kind: ModelKind("future")},
	}
	for index, snapshot := range invalid {
		if err := snapshot.Validate(); err == nil {
			t.Fatalf("invalid snapshot %d was accepted", index)
		}
	}

	for _, revision := range []string{"", strings.Repeat("a", 129), ".v1", "v/1"} {
		if _, err := NewLinearSnapshot(revision, linear); err == nil {
			t.Fatalf("revision %q was accepted", revision)
		}
	}
	if !validRevision("9.2-rc_1") {
		t.Fatalf("valid revision was rejected")
	}
	if _, err := NewLinearSnapshot("v1", wrongLinear); err == nil {
		t.Fatalf("wrong linear catalog was accepted")
	}
	if _, err := NewHistogramSnapshot("v1", wrongHistogram); err == nil {
		t.Fatalf("wrong histogram catalog was accepted")
	}
}

func TestSnapshotJSONRoundTripsBothModels(t *testing.T) {
	snapshots := []Snapshot{
		mustSnapshot(t, "linear-v2", mustLinearModel(t, linearWeights(map[int]float64{0: 1}))),
		mustHistogramSnapshot(t, "tree-v2", mustHistogramModel(t)),
	}
	for _, original := range snapshots {
		encoded, err := json.Marshal(original)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		parsed, err := ParseSnapshot(encoded)
		if err != nil {
			t.Fatalf("ParseSnapshot: %v", err)
		}
		if parsed.Revision() != original.Revision() || parsed.Kind() != original.Kind() {
			t.Fatalf("round trip metadata = %q, %q", parsed.Revision(), parsed.Kind())
		}
		encodedAgain, err := json.Marshal(parsed)
		if err != nil || string(encodedAgain) != string(encoded) {
			t.Fatalf("deterministic JSON = %s, %v", encodedAgain, err)
		}
		var unmarshaled Snapshot
		if err := json.Unmarshal(encoded, &unmarshaled); err != nil ||
			unmarshaled.Revision() != original.Revision() {
			t.Fatalf("Unmarshal = %#v, %v", unmarshaled, err)
		}
	}
}

func TestSnapshotJSONPreservesLegacyNestedModelFormats(t *testing.T) {
	snapshots := []Snapshot{
		mustSnapshot(t, "linear-legacy", mustLinearModel(t, linearWeights(nil))),
		mustHistogramSnapshot(t, "tree-legacy", mustHistogramModel(t)),
	}
	replacements := [][2]string{
		{"yago-linear-lambdarank-v2", "yago-linear-lambdarank-v1"},
		{"yago-histogram-lambdamart-v2", "yago-histogram-lambdamart-v1"},
	}
	for index, snapshot := range snapshots {
		encoded, err := json.Marshal(snapshot)
		if err != nil {
			t.Fatalf("marshal snapshot: %v", err)
		}
		legacy := strings.Replace(
			string(encoded),
			replacements[index][0],
			replacements[index][1],
			1,
		)
		parsed, err := ParseSnapshot([]byte(legacy))
		if err != nil {
			t.Fatalf("parse legacy snapshot: %v", err)
		}
		encodedAgain, err := json.Marshal(parsed)
		if err != nil || string(encodedAgain) != legacy {
			t.Fatalf("legacy snapshot changed: %s, %v", encodedAgain, err)
		}
	}
}

func TestSnapshotJSONRejectsMalformedAndUnsupportedDataTransactionally(t *testing.T) {
	valid := mustSnapshot(t, "stable", mustLinearModel(t, linearWeights(nil)))
	validJSON, err := json.Marshal(valid)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	invalid := [][]byte{
		nil,
		make([]byte, MaximumSnapshotBytes+1),
		[]byte(`{`),
		[]byte(
			`{"format":"yago-learned-rank-snapshot-v1",` +
				`"revision":"v1","model_kind":"linear_lambdarank",` +
				`"model":{},"extra":1}`,
		),
		append(append([]byte(nil), validJSON...), []byte(` {}`)...),
		append(append([]byte(nil), validJSON...), []byte(` {`)...),
		snapshotDocumentJSON(t, "future", "v1", ModelLinearLambdaRank, `{}`),
		snapshotDocumentJSON(t, SnapshotJSONFormat, "v1", ModelKind("future"), `{}`),
		snapshotDocumentJSON(t, SnapshotJSONFormat, "v1", ModelLinearLambdaRank, `[]`),
		snapshotDocumentJSON(t, SnapshotJSONFormat, "v1", ModelHistogramLambdaMART, `[]`),
		snapshotDocumentJSON(
			t,
			SnapshotJSONFormat,
			".bad",
			ModelLinearLambdaRank,
			`{"format":"yago-linear-lambdarank-v1","features":[],"weights":[]}`,
		),
	}
	for index, data := range invalid {
		if _, err := ParseSnapshot(data); err == nil {
			t.Fatalf("invalid JSON %d was accepted", index)
		}
		preserved := valid
		if err := json.Unmarshal(data, &preserved); err == nil {
			t.Fatalf("invalid JSON %d unmarshaled", index)
		}
		if preserved.Revision() != valid.Revision() {
			t.Fatalf("invalid JSON %d mutated the snapshot", index)
		}
	}
}

func TestSnapshotMarshalRejectsInvalidModelStates(t *testing.T) {
	linear := mustLinearModel(t, linearWeights(nil))
	histogram := mustHistogramModel(t)
	invalid := []Snapshot{
		{revision: "v1", kind: ModelLinearLambdaRank},
		{revision: "v1", kind: ModelHistogramLambdaMART},
		{revision: "v1", kind: ModelKind("future")},
		{
			revision: "v1",
			kind:     ModelLinearLambdaRank,
			linear:   &rankfit.LinearLambdaRankModel{},
		},
		{
			revision:  "v1",
			kind:      ModelHistogramLambdaMART,
			histogram: &rankfit.HistogramLambdaMARTModel{},
		},
		{
			revision:  "v1",
			kind:      ModelLinearLambdaRank,
			linear:    &linear,
			histogram: &histogram,
		},
	}
	for index, snapshot := range invalid {
		if _, err := json.Marshal(snapshot); err == nil {
			t.Fatalf("invalid snapshot %d marshaled", index)
		}
	}
}

func mustSnapshot(
	t *testing.T,
	revision string,
	model rankfit.LinearLambdaRankModel,
) Snapshot {
	t.Helper()
	snapshot, err := NewLinearSnapshot(revision, model)
	if err != nil {
		t.Fatalf("NewLinearSnapshot: %v", err)
	}

	return snapshot
}

func mustHistogramSnapshot(
	t *testing.T,
	revision string,
	model rankfit.HistogramLambdaMARTModel,
) Snapshot {
	t.Helper()
	snapshot, err := NewHistogramSnapshot(revision, model)
	if err != nil {
		t.Fatalf("NewHistogramSnapshot: %v", err)
	}

	return snapshot
}

func snapshotDocumentJSON(
	t *testing.T,
	format string,
	revision string,
	kind ModelKind,
	model string,
) []byte {
	t.Helper()
	encoded, err := json.Marshal(snapshotDocument{
		Format:   format,
		Revision: revision,
		Kind:     kind,
		Model:    json.RawMessage(model),
	})
	if err != nil {
		t.Fatalf("Marshal snapshot document: %v", err)
	}

	return encoded
}
