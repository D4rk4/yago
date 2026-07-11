package peerreputation

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type SignedPeerIdentity string

type NetworkGroupKey string

type Outcome uint8

const (
	OutcomeSuccess Outcome = iota + 1
	OutcomeFailure
	OutcomeTimeout
	OutcomeInvalidResult
)

type Observation struct {
	Peer         SignedPeerIdentity
	NetworkGroup NetworkGroupKey
	Outcome      Outcome
	ObservedAt   time.Time
}

type ObservationBatch struct {
	Sequence     uint64
	Observations []Observation
}

type BatchApplication struct {
	Applied       bool
	Superseded    bool
	LastSequence  uint64
	RetainedPeers int
}

type normalizedObservation struct {
	peer            SignedPeerIdentity
	networkGroup    NetworkGroupKey
	outcome         Outcome
	observedAtNanos int64
}

func normalizeBatch(batch ObservationBatch) ([]normalizedObservation, string, error) {
	if batch.Sequence == 0 {
		return nil, "", fmt.Errorf("peer reputation batch sequence must be positive")
	}
	if len(batch.Observations) == 0 || len(batch.Observations) > maximumBatchObservations {
		return nil, "", fmt.Errorf("peer reputation batch size is invalid")
	}

	normalized := make([]normalizedObservation, len(batch.Observations))
	for index, observation := range batch.Observations {
		if err := validateBoundedLabel(
			string(observation.Peer),
			"signed peer identity",
		); err != nil {
			return nil, "", err
		}
		if err := validateBoundedLabel(
			string(observation.NetworkGroup),
			"network group",
		); err != nil {
			return nil, "", err
		}
		if _, _, ok := outcomeEvidence(observation.Outcome); !ok {
			return nil, "", fmt.Errorf("peer reputation outcome is invalid")
		}
		observedAtNanos, err := validatedUnixNanos(observation.ObservedAt)
		if err != nil {
			return nil, "", err
		}
		normalized[index] = normalizedObservation{
			peer:            observation.Peer,
			networkGroup:    observation.NetworkGroup,
			outcome:         observation.Outcome,
			observedAtNanos: observedAtNanos,
		}
	}

	sort.Slice(normalized, func(left, right int) bool {
		return observationLess(normalized[left], normalized[right])
	})

	return normalized, batchFingerprint(batch.Sequence, normalized), nil
}

func observationLess(left, right normalizedObservation) bool {
	if left.peer != right.peer {
		return left.peer < right.peer
	}
	if left.observedAtNanos != right.observedAtNanos {
		return left.observedAtNanos < right.observedAtNanos
	}
	if left.networkGroup != right.networkGroup {
		return left.networkGroup < right.networkGroup
	}

	return left.outcome < right.outcome
}

func batchFingerprint(sequence uint64, observations []normalizedObservation) string {
	canonical := make([]byte, 0, 16+len(observations)*64)
	canonical = binary.BigEndian.AppendUint64(canonical, sequence)
	canonical = fmt.Appendf(canonical, "%d;", len(observations))
	for _, observation := range observations {
		canonical = appendCanonicalString(canonical, string(observation.peer))
		canonical = appendCanonicalString(canonical, string(observation.networkGroup))
		canonical = append(canonical, byte(observation.outcome))
		canonical = strconv.AppendInt(canonical, observation.observedAtNanos, 10)
		canonical = append(canonical, ';')
	}
	fingerprint := sha256.Sum256(canonical)

	return hex.EncodeToString(fingerprint[:])
}

func appendCanonicalString(canonical []byte, value string) []byte {
	canonical = fmt.Appendf(canonical, "%d:", len(value))

	return append(canonical, value...)
}

func validateBoundedLabel(value string, label string) error {
	if value == "" || len(value) > maximumPeerLabelBytes || !utf8.ValidString(value) ||
		strings.TrimSpace(value) != value {
		return fmt.Errorf("peer reputation %s is invalid", label)
	}

	return nil
}

func validatedUnixNanos(value time.Time) (int64, error) {
	nanos := value.UnixNano()
	if value.IsZero() || nanos <= 0 || !time.Unix(0, nanos).Equal(value) {
		return 0, fmt.Errorf("peer reputation observation time is invalid")
	}

	return nanos, nil
}

func outcomeEvidence(outcome Outcome) (float64, float64, bool) {
	switch outcome {
	case OutcomeSuccess:
		return 1, 0, true
	case OutcomeFailure:
		return 0, 1, true
	case OutcomeTimeout:
		return 0, 0.5, true
	case OutcomeInvalidResult:
		return 0, 2, true
	default:
		return 0, 0, false
	}
}
