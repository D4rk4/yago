package contentcluster

import (
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
