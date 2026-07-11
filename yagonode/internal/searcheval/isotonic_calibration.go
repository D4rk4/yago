package searcheval

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
)

type IsotonicModel struct {
	Thresholds    []float64 `json:"thresholds"`
	Probabilities []float64 `json:"probabilities"`
}

type isotonicBlock struct {
	threshold float64
	positive  float64
	weight    float64
}

func FitIsotonicCalibration(samples []CalibrationSample) (IsotonicModel, error) {
	if _, _, _, _, err := calibrationSampleSummary(samples, false); err != nil {
		return IsotonicModel{}, err
	}
	sorted := append([]CalibrationSample(nil), samples...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Score < sorted[j].Score })
	blocks := make([]isotonicBlock, 0, len(sorted))
	for index := 0; index < len(sorted); {
		threshold := sorted[index].Score
		positive := 0.0
		weight := 0.0
		for index < len(sorted) && sorted[index].Score == threshold {
			if sorted[index].Relevant {
				positive++
			}
			weight++
			index++
		}
		blocks = append(blocks, isotonicBlock{
			threshold: threshold,
			positive:  positive,
			weight:    weight,
		})
		for len(blocks) >= 2 && isotonicBlockProbability(blocks[len(blocks)-2]) >
			isotonicBlockProbability(blocks[len(blocks)-1]) {
			last := blocks[len(blocks)-1]
			previous := &blocks[len(blocks)-2]
			previous.threshold = last.threshold
			previous.positive += last.positive
			previous.weight += last.weight
			blocks = blocks[:len(blocks)-1]
		}
	}
	model := IsotonicModel{
		Thresholds:    make([]float64, len(blocks)),
		Probabilities: make([]float64, len(blocks)),
	}
	for index, block := range blocks {
		model.Thresholds[index] = block.threshold
		model.Probabilities[index] = isotonicBlockProbability(block)
	}
	return model, model.Validate()
}

func (m IsotonicModel) Probability(score float64) float64 {
	if math.IsNaN(score) {
		return 0.5
	}
	index := sort.SearchFloat64s(m.Thresholds, score)
	if index >= len(m.Probabilities) {
		return m.Probabilities[len(m.Probabilities)-1]
	}

	return m.Probabilities[index]
}

func (m IsotonicModel) Validate() error {
	if len(m.Thresholds) == 0 || len(m.Thresholds) != len(m.Probabilities) {
		return fmt.Errorf("isotonic thresholds and probabilities must have equal non-zero length")
	}
	for index := range m.Thresholds {
		if math.IsNaN(m.Thresholds[index]) || math.IsInf(m.Thresholds[index], 0) {
			return fmt.Errorf("isotonic threshold must be finite")
		}
		if index > 0 && m.Thresholds[index] <= m.Thresholds[index-1] {
			return fmt.Errorf("isotonic thresholds must be strictly increasing")
		}
		probability := m.Probabilities[index]
		if math.IsNaN(probability) || math.IsInf(probability, 0) ||
			probability < 0 || probability > 1 {
			return fmt.Errorf("isotonic probability must be finite and in [0,1]")
		}
		if index > 0 && probability < m.Probabilities[index-1] {
			return fmt.Errorf("isotonic probabilities must be non-decreasing")
		}
	}

	return nil
}

func (m IsotonicModel) MarshalJSON() ([]byte, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	data := []byte(`{"thresholds":[`)
	data = appendCalibrationValues(data, m.Thresholds)
	data = append(data, `],"probabilities":[`...)
	data = appendCalibrationValues(data, m.Probabilities)
	data = append(data, ']', '}')

	return data, nil
}

func (m *IsotonicModel) UnmarshalJSON(data []byte) error {
	type wire IsotonicModel
	var decoded wire
	if err := json.Unmarshal(data, &decoded); err != nil {
		return fmt.Errorf("decode isotonic model: %w", err)
	}
	model := IsotonicModel(decoded)
	if err := model.Validate(); err != nil {
		return err
	}
	model.Thresholds = append([]float64(nil), model.Thresholds...)
	model.Probabilities = append([]float64(nil), model.Probabilities...)
	*m = model

	return nil
}

func ParseIsotonicModelJSON(data []byte) (IsotonicModel, error) {
	var model IsotonicModel
	if err := json.Unmarshal(data, &model); err != nil {
		return IsotonicModel{}, fmt.Errorf("parse isotonic model: %w", err)
	}

	return model, nil
}

func isotonicBlockProbability(block isotonicBlock) float64 {
	return block.positive / block.weight
}

func appendCalibrationValues(data []byte, values []float64) []byte {
	for index, value := range values {
		if index > 0 {
			data = append(data, ',')
		}
		data = strconv.AppendFloat(data, value, 'g', -1, 64)
	}

	return data
}
