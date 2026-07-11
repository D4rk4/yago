package learnedrank

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"

	"github.com/D4rk4/yago/yagonode/internal/rankfit"
)

const (
	SnapshotJSONFormat   = "yago-learned-rank-snapshot-v1"
	MaximumSnapshotBytes = 1 << 20
)

type snapshotDocument struct {
	Format   string          `json:"format"`
	Revision string          `json:"revision"`
	Kind     ModelKind       `json:"model_kind"`
	Model    json.RawMessage `json:"model"`
}

func ParseSnapshot(data []byte) (Snapshot, error) {
	if len(data) == 0 || len(data) > MaximumSnapshotBytes {
		return Snapshot{}, fmt.Errorf("model snapshot JSON size is invalid")
	}
	var document snapshotDocument
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		return Snapshot{}, fmt.Errorf("decode model snapshot: %w", err)
	}
	if err := requireSnapshotJSONEnd(decoder); err != nil {
		return Snapshot{}, err
	}
	if document.Format != SnapshotJSONFormat {
		return Snapshot{}, fmt.Errorf("unsupported model snapshot format %q", document.Format)
	}
	switch document.Kind {
	case ModelLinearLambdaRank:
		var model rankfit.LinearLambdaRankModel
		if err := json.Unmarshal(document.Model, &model); err != nil {
			return Snapshot{}, fmt.Errorf("decode linear LambdaRank snapshot model: %w", err)
		}

		return NewLinearSnapshot(document.Revision, model)
	case ModelHistogramLambdaMART:
		var model rankfit.HistogramLambdaMARTModel
		if err := json.Unmarshal(document.Model, &model); err != nil {
			return Snapshot{}, fmt.Errorf("decode histogram LambdaMART snapshot model: %w", err)
		}

		return NewHistogramSnapshot(document.Revision, model)
	default:
		return Snapshot{}, fmt.Errorf("model kind %q is unsupported", document.Kind)
	}
}

func (s Snapshot) MarshalJSON() ([]byte, error) {
	var model []byte
	var err error
	switch s.kind {
	case ModelLinearLambdaRank:
		if s.linear == nil {
			return nil, fmt.Errorf("linear LambdaRank snapshot has invalid model state")
		}
		model, err = json.Marshal(s.linear)
	case ModelHistogramLambdaMART:
		if s.histogram == nil {
			return nil, fmt.Errorf("histogram LambdaMART snapshot has invalid model state")
		}
		model, err = json.Marshal(s.histogram)
	default:
		return nil, fmt.Errorf("model kind %q is unsupported", s.kind)
	}
	if err != nil {
		return nil, fmt.Errorf("encode snapshot model: %w", err)
	}
	if err := s.Validate(); err != nil {
		return nil, err
	}
	encoded := append([]byte(`{"format":`), strconv.Quote(SnapshotJSONFormat)...)
	encoded = append(encoded, `,"revision":`...)
	encoded = append(encoded, strconv.Quote(s.revision)...)
	encoded = append(encoded, `,"model_kind":`...)
	encoded = append(encoded, strconv.Quote(string(s.kind))...)
	encoded = append(encoded, `,"model":`...)
	encoded = append(encoded, model...)
	encoded = append(encoded, '}')

	return encoded, nil
}

func (s *Snapshot) UnmarshalJSON(data []byte) error {
	snapshot, err := ParseSnapshot(data)
	if err != nil {
		return err
	}
	*s = snapshot

	return nil
}

func requireSnapshotJSONEnd(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("decode model snapshot: trailing JSON value")
		}

		return fmt.Errorf("decode model snapshot: %w", err)
	}

	return nil
}
