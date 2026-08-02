package contentcluster

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const (
	fingerprintBucketName vault.Name = "content_cluster_fingerprints_v1"
	clusterBucketName     vault.Name = "content_cluster_members_v1"
	exactBucketName       vault.Name = "content_cluster_exact_v1"
	bandBucketName        vault.Name = "content_cluster_bands_v1"
)

type fingerprintRecord struct {
	URL                string   `json:"url"`
	ContentHash        string   `json:"content_hash"`
	Fingerprint        uint64   `json:"fingerprint"`
	Shingles           []uint64 `json:"shingles"`
	ClusterID          string   `json:"cluster_id"`
	CanonicalPreferred bool     `json:"canonical_preferred"`
	Quality            float64  `json:"quality"`
	InboundAuthority   float64  `json:"inbound_authority"`
}

// fingerprintMatch carries the three quantities posting visibility compares,
// and nothing else. It mirrors the stored fingerprintRecord field for field, so
// their JSON tags must not drift apart -- TestFingerprintMatchMirrorsTheRecord
// is what holds them together.
//
// The shingle vector is reduced to whether it is empty, because that is all
// postingMatches asks of it. Materializing it was the dominant cost of writing
// a page: every posting bucket a record belongs to is re-read to drop members
// that no longer match, so one page costs up to (bandCount+1) buckets times
// MaximumBucketMembers members, and each of those members carried up to
// MaximumShingles values that were parsed into a slice and immediately
// discarded.
type fingerprintMatch struct {
	ContentHash string          `json:"content_hash"`
	Fingerprint uint64          `json:"fingerprint"`
	HasShingles shinglePresence `json:"shingles"`
}

// fingerprintTransitionMatch is fingerprintTransition reduced the same way. A
// transition entry embeds two whole fingerprints, so reading one through the
// full shape parsed two shingle vectors to reach the identity fields and one
// record's three scalars.
type fingerprintTransitionMatch struct {
	Token        string           `json:"token"`
	URL          string           `json:"url"`
	Current      fingerprintMatch `json:"current"`
	CurrentFound bool             `json:"current_found"`
}

// shinglePresence records whether a stored shingle array holds anything,
// without building it. encoding/json hands an Unmarshaler the raw bytes of the
// value as a sub-slice of the buffer it is already decoding, so this allocates
// nothing and converts no number -- what it costs is the scan that finds the
// array's end, which the decoder performs either way.
//
// It deliberately refuses a value that is neither null nor an array, so a
// record that could not decode into []uint64 does not quietly decode here. What
// it does not do is check the elements: [[1]] reads as present where []uint64
// would fail. Records are written by this package from a []uint64, so no such
// value can be stored; the check is shallow because it is a corruption guard,
// not a parser.
type shinglePresence bool

func (p *shinglePresence) UnmarshalJSON(raw []byte) error {
	trimmed := bytes.TrimSpace(raw)
	if string(trimmed) == "null" {
		*p = false

		return nil
	}
	if len(trimmed) < 2 || trimmed[0] != '[' || trimmed[len(trimmed)-1] != ']' {
		return fmt.Errorf("content fingerprint shingles must be a JSON array")
	}
	*p = len(bytes.TrimSpace(trimmed[1:len(trimmed)-1])) > 0

	return nil
}

type representativeRecord struct {
	URL                string  `json:"url"`
	CanonicalPreferred bool    `json:"canonical_preferred"`
	Quality            float64 `json:"quality"`
	InboundAuthority   float64 `json:"inbound_authority"`
}

type clusterRecord struct {
	ID             string               `json:"id"`
	Members        []string             `json:"members"`
	Representative representativeRecord `json:"representative"`
}

type postingRecord struct {
	URLs []string `json:"urls"`
}

// rawJSONCodec stores an already-encoded JSON document unchanged.
//
// The fingerprint keyspace holds values that its own readers parse, so it was
// registered with jsonCodec[json.RawMessage]. That made the vault run
// json.Unmarshal on every read purely to produce a json.RawMessage -- which
// scans and validates the whole document, then copies it -- and hand those same
// bytes straight to a second Unmarshal in readFingerprintShape. Every stored
// fingerprint was therefore parsed twice per read, and a stored record averages
// 18.7 KB because it carries the shingle vector.
//
// Decode still returns a copy. The bytes underneath may be a window into the
// storage engine's mapping, valid only for the enclosing transaction, and the
// jsonCodec this replaces copied them too; keeping the copy leaves every
// caller's assumption about lifetime exactly as it was. What is dropped is the
// validation, and a memcpy of the value is some two orders of magnitude cheaper
// than a scan of it.
//
// Encode returns the value as given rather than marshalling it. Both writers
// build their bytes with json.Marshal on a struct, so the document is valid by
// construction and re-validating it on the way in bought nothing.
type rawJSONCodec struct{}

func (rawJSONCodec) Encode(value json.RawMessage) ([]byte, error) {
	return value, nil
}

func (rawJSONCodec) Decode(raw []byte) (json.RawMessage, error) {
	return append(json.RawMessage(nil), raw...), nil
}

type jsonCodec[Value any] struct{}

func (jsonCodec[Value]) Encode(value Value) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode JSON: %w", err)
	}

	return raw, nil
}

func (jsonCodec[Value]) Decode(raw []byte) (Value, error) {
	var value Value
	if err := json.Unmarshal(raw, &value); err != nil {
		return value, fmt.Errorf("decode JSON: %w", err)
	}

	return value, nil
}

func recordFrom(prepared preparedEvidence, clusterID string) fingerprintRecord {
	return fingerprintRecord{
		URL:                prepared.URL,
		ContentHash:        prepared.ContentHash,
		Fingerprint:        prepared.Fingerprint,
		Shingles:           append([]uint64(nil), prepared.Shingles...),
		ClusterID:          clusterID,
		CanonicalPreferred: prepared.CanonicalPreferred,
		Quality:            prepared.Quality,
		InboundAuthority:   prepared.InboundAuthority,
	}
}

func representativeFrom(record fingerprintRecord) representativeRecord {
	return representativeRecord{
		URL:                record.URL,
		CanonicalPreferred: record.CanonicalPreferred,
		Quality:            record.Quality,
		InboundAuthority:   record.InboundAuthority,
	}
}

func assignmentFrom(cluster clusterRecord) Assignment {
	return Assignment{
		ClusterID:         cluster.ID,
		RepresentativeURL: cluster.Representative.URL,
	}
}
