package yagocrawlcontract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"
	"unicode/utf8"
)

const (
	MaximumRecentCrawlURLOutcomes  = 64
	MaximumCrawlOutcomeURLBytes    = 2048
	MaximumCrawlOutcomeReasonBytes = 160
)

type CrawlURLOutcomeClass string

const (
	CrawlURLOutcomeFetched      CrawlURLOutcomeClass = "fetched"
	CrawlURLOutcomeIndexed      CrawlURLOutcomeClass = "indexed"
	CrawlURLOutcomeFailed       CrawlURLOutcomeClass = "failed"
	CrawlURLOutcomeRobotsDenied CrawlURLOutcomeClass = "robots_denied"
	CrawlURLOutcomeDuplicate    CrawlURLOutcomeClass = "duplicate"
	CrawlURLOutcomeSkipped      CrawlURLOutcomeClass = "skipped"
)

type CrawlURLOutcome struct {
	Sequence        uint64               `json:"sequence"`
	URL             string               `json:"url"`
	Class           CrawlURLOutcomeClass `json:"class"`
	ObservedAt      time.Time            `json:"observedAt"`
	HTTPStatus      uint32               `json:"httpStatus,omitempty"`
	Reason          string               `json:"reason,omitempty"`
	WorkerSessionID string               `json:"workerSessionId,omitempty"`
}

func (outcome CrawlURLOutcome) Valid() bool {
	return outcome.Sequence > 0 && outcome.URL != "" &&
		len(outcome.URL) <= MaximumCrawlOutcomeURLBytes && utf8.ValidString(outcome.URL) &&
		validCrawlURLOutcomeClass(outcome.Class) && !outcome.ObservedAt.IsZero() &&
		outcome.HTTPStatus <= 999 &&
		len(outcome.Reason) <= MaximumCrawlOutcomeReasonBytes && utf8.ValidString(outcome.Reason) &&
		(outcome.WorkerSessionID == "" ||
			ValidCrawlerSessionIdentity(outcome.WorkerSessionID))
}

func validCrawlURLOutcomeClass(class CrawlURLOutcomeClass) bool {
	switch class {
	case CrawlURLOutcomeFetched,
		CrawlURLOutcomeIndexed,
		CrawlURLOutcomeFailed,
		CrawlURLOutcomeRobotsDenied,
		CrawlURLOutcomeDuplicate,
		CrawlURLOutcomeSkipped:
		return true
	default:
		return false
	}
}

type CrawlURLOutcomeHistory struct {
	entries [MaximumRecentCrawlURLOutcomes]CrawlURLOutcome
	length  uint8
}

func NewCrawlURLOutcomeHistory(outcomes []CrawlURLOutcome) (CrawlURLOutcomeHistory, error) {
	var history CrawlURLOutcomeHistory
	if len(outcomes) > MaximumRecentCrawlURLOutcomes {
		return history, fmt.Errorf(
			"crawl URL outcome history exceeds %d entries",
			MaximumRecentCrawlURLOutcomes,
		)
	}
	for _, outcome := range outcomes {
		if !outcome.Valid() {
			return CrawlURLOutcomeHistory{}, fmt.Errorf("invalid crawl URL outcome")
		}
		history.Append(outcome)
	}

	return history, nil
}

func (history *CrawlURLOutcomeHistory) Append(outcome CrawlURLOutcome) {
	if history.length < MaximumRecentCrawlURLOutcomes {
		history.entries[history.length] = outcome
		history.length++

		return
	}
	copy(history.entries[:], history.entries[1:])
	history.entries[MaximumRecentCrawlURLOutcomes-1] = outcome
}

func (history CrawlURLOutcomeHistory) Chronological() []CrawlURLOutcome {
	outcomes := make([]CrawlURLOutcome, int(history.length))
	copy(outcomes, history.entries[:history.length])

	return outcomes
}

func (history CrawlURLOutcomeHistory) NewestFirst() []CrawlURLOutcome {
	outcomes := history.Chronological()
	for left, right := 0, len(outcomes)-1; left < right; left, right = left+1, right-1 {
		outcomes[left], outcomes[right] = outcomes[right], outcomes[left]
	}

	return outcomes
}

func (history CrawlURLOutcomeHistory) Valid() bool {
	for _, outcome := range history.Chronological() {
		if !outcome.Valid() {
			return false
		}
	}

	return true
}

func (history CrawlURLOutcomeHistory) Merge(
	incoming CrawlURLOutcomeHistory,
) CrawlURLOutcomeHistory {
	merged := history
	for _, candidate := range incoming.Chronological() {
		if !merged.contains(candidate) {
			merged.Append(candidate)
		}
	}

	return merged
}

func (history CrawlURLOutcomeHistory) WithWorkerSessionID(
	workerSessionID string,
) CrawlURLOutcomeHistory {
	for index := range int(history.length) {
		history.entries[index].WorkerSessionID = workerSessionID
	}

	return history
}

func (history CrawlURLOutcomeHistory) contains(candidate CrawlURLOutcome) bool {
	for index := range int(history.length) {
		stored := history.entries[index]
		if stored.WorkerSessionID == candidate.WorkerSessionID &&
			stored.Sequence == candidate.Sequence {
			return true
		}
	}

	return false
}

func (history CrawlURLOutcomeHistory) MarshalJSON() ([]byte, error) {
	raw, err := json.Marshal(history.Chronological())
	if err != nil {
		return nil, fmt.Errorf("encode crawl URL outcome history: %w", err)
	}

	return raw, nil
}

func (history *CrawlURLOutcomeHistory) UnmarshalJSON(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var outcomes []CrawlURLOutcome
	if err := decoder.Decode(&outcomes); err != nil {
		return fmt.Errorf("decode crawl URL outcome history: %w", err)
	}
	decoded, err := NewCrawlURLOutcomeHistory(outcomes)
	if err != nil {
		return err
	}
	*history = decoded

	return nil
}
