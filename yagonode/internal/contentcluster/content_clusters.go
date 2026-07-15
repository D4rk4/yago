package contentcluster

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const (
	defaultMaximumTextBytes      = 1 << 20
	defaultMaximumShingles       = 4096
	defaultMaximumCandidates     = 128
	defaultMaximumBucketMembers  = 256
	defaultMaximumClusterMembers = 1024
	defaultShingleWords          = 4
	defaultMinimumJaccard        = 0.8
	hardMaximumTextBytes         = 16 << 20
	hardMaximumShingles          = 65536
	hardMaximumCandidates        = 4096
	hardMaximumBucketMembers     = 4096
	hardMaximumClusterMembers    = 65536
	hardMaximumShingleWords      = 16
	maximumURLBytes              = 8192
	maximumContentHashBytes      = 512
)

var ErrInvalidEvidence = errors.New("invalid content cluster evidence")

type Limits struct {
	MaximumTextBytes      int
	MaximumShingles       int
	MaximumCandidates     int
	MaximumBucketMembers  int
	MaximumClusterMembers int
	ShingleWords          int
	MinimumJaccard        float64
}

type Evidence struct {
	URL                string
	Text               string
	ContentHash        string
	CanonicalPreferred bool
	Quality            float64
	InboundAuthority   float64
}

type Assignment struct {
	ClusterID         string
	RepresentativeURL string
}

type Cluster struct {
	ID                string
	RepresentativeURL string
	MemberURLs        []string
}

type Index struct {
	vault               *vault.Vault
	limits              Limits
	fingerprints        *fingerprintKeyspace
	clusters            *vault.Keyspace[clusterRecord]
	exactBuckets        *vault.Keyspace[postingRecord]
	bandBuckets         *vault.Keyspace[postingRecord]
	boundaries          *evidenceBoundaries
	candidateBoundaries *evidenceBoundaries
	projections         *evidenceBoundaries
}

func DefaultLimits() Limits {
	return Limits{
		MaximumTextBytes:      defaultMaximumTextBytes,
		MaximumShingles:       defaultMaximumShingles,
		MaximumCandidates:     defaultMaximumCandidates,
		MaximumBucketMembers:  defaultMaximumBucketMembers,
		MaximumClusterMembers: defaultMaximumClusterMembers,
		ShingleWords:          defaultShingleWords,
		MinimumJaccard:        defaultMinimumJaccard,
	}
}

func Open(v *vault.Vault, requested Limits) (*Index, error) {
	limits, err := completeLimits(requested)
	if err != nil {
		return nil, err
	}
	fingerprints, err := registerFingerprintKeyspace(v)
	if err != nil {
		return nil, fmt.Errorf("register content fingerprints: %w", err)
	}
	clusters, err := vault.RegisterKeyspace(v, clusterBucketName, jsonCodec[clusterRecord]{})
	if err != nil {
		return nil, fmt.Errorf("register content clusters: %w", err)
	}
	exactBuckets, err := vault.RegisterKeyspace(v, exactBucketName, jsonCodec[postingRecord]{})
	if err != nil {
		return nil, fmt.Errorf("register exact content buckets: %w", err)
	}
	bandBuckets, err := vault.RegisterKeyspace(v, bandBucketName, jsonCodec[postingRecord]{})
	if err != nil {
		return nil, fmt.Errorf("register content fingerprint bands: %w", err)
	}

	return &Index{
		vault:               v,
		limits:              limits,
		fingerprints:        fingerprints,
		clusters:            clusters,
		exactBuckets:        exactBuckets,
		bandBuckets:         bandBuckets,
		boundaries:          newEvidenceBoundaries(),
		candidateBoundaries: newEvidenceBoundaries(),
		projections:         newEvidenceBoundaries(),
	}, nil
}

func (i *Index) Replace(ctx context.Context, evidence Evidence) (Assignment, error) {
	prepared, err := prepareEvidence(ctx, i.limits, evidence)
	if err != nil {
		return Assignment{}, err
	}
	replacements, err := i.replacePreparedBatch(ctx, []preparedEvidence{prepared})
	if err != nil {
		return Assignment{}, fmt.Errorf("replace content cluster evidence: %w", err)
	}
	if replacements[0].Finalization.token != "" {
		if err := i.FinalizeEvidenceTransitions(
			ctx,
			[]EvidenceFinalization{replacements[0].Finalization},
		); err != nil {
			i.ReleaseEvidenceTransitions([]EvidenceFinalization{
				replacements[0].Finalization,
			})

			return Assignment{}, fmt.Errorf("finalize content cluster evidence: %w", err)
		}
	}

	return replacements[0].Current, nil
}

func (i *Index) Delete(ctx context.Context, url string) (bool, error) {
	deletion, err := i.DeleteTransition(ctx, url)
	if err != nil {
		return false, err
	}
	if deletion.Finalization.token != "" {
		if err := i.FinalizeEvidenceTransitions(
			ctx,
			[]EvidenceFinalization{deletion.Finalization},
		); err != nil {
			i.ReleaseEvidenceTransitions([]EvidenceFinalization{
				deletion.Finalization,
			})

			return false, fmt.Errorf("finalize deleted content cluster evidence: %w", err)
		}
	}

	return deletion.Deleted, nil
}

func (i *Index) Lookup(ctx context.Context, url string) (Assignment, bool, error) {
	normalizedURL, err := validateURL(url)
	if err != nil {
		return Assignment{}, false, err
	}
	var assignment Assignment
	var found bool
	err = i.vault.View(ctx, func(tx *vault.Txn) error {
		record, exists, readErr := i.fingerprints.Get(tx, vault.Key(normalizedURL))
		if readErr != nil {
			return fmt.Errorf("read content fingerprint: %w", readErr)
		}
		if !exists {
			return nil
		}
		cluster, exists, readErr := i.publishedCluster(tx, ctx, record.ClusterID)
		if readErr != nil {
			return fmt.Errorf("read content cluster: %w", readErr)
		}
		if !exists {
			return fmt.Errorf("content cluster %q is missing", record.ClusterID)
		}
		assignment = assignmentFrom(cluster)
		found = true

		return nil
	})
	if err != nil {
		return Assignment{}, false, fmt.Errorf("look up content cluster: %w", err)
	}

	return assignment, found, nil
}

func (i *Index) Cluster(ctx context.Context, clusterID string) (Cluster, bool, error) {
	clusterID = strings.TrimSpace(clusterID)
	if clusterID == "" {
		return Cluster{}, false, ErrInvalidEvidence
	}
	var cluster Cluster
	var found bool
	err := i.vault.View(ctx, func(tx *vault.Txn) error {
		record, exists, readErr := i.publishedCluster(tx, ctx, clusterID)
		if readErr != nil {
			return fmt.Errorf("read content cluster: %w", readErr)
		}
		if !exists {
			return nil
		}
		cluster = Cluster{
			ID:                record.ID,
			RepresentativeURL: record.Representative.URL,
			MemberURLs:        append([]string(nil), record.Members...),
		}
		found = true

		return nil
	})
	if err != nil {
		return Cluster{}, false, fmt.Errorf("get content cluster: %w", err)
	}

	return cluster, found, nil
}

func completeLimits(requested Limits) (Limits, error) {
	limits := requested
	defaults := DefaultLimits()
	if limits.MaximumTextBytes == 0 {
		limits.MaximumTextBytes = defaults.MaximumTextBytes
	}
	if limits.MaximumShingles == 0 {
		limits.MaximumShingles = defaults.MaximumShingles
	}
	if limits.MaximumCandidates == 0 {
		limits.MaximumCandidates = defaults.MaximumCandidates
	}
	if limits.MaximumBucketMembers == 0 {
		limits.MaximumBucketMembers = defaults.MaximumBucketMembers
	}
	if limits.MaximumClusterMembers == 0 {
		limits.MaximumClusterMembers = defaults.MaximumClusterMembers
	}
	if limits.ShingleWords == 0 {
		limits.ShingleWords = defaults.ShingleWords
	}
	if limits.MinimumJaccard == 0 {
		limits.MinimumJaccard = defaults.MinimumJaccard
	}
	checks := []struct {
		name  string
		value int
		limit int
	}{
		{"maximum text bytes", limits.MaximumTextBytes, hardMaximumTextBytes},
		{"maximum shingles", limits.MaximumShingles, hardMaximumShingles},
		{"maximum candidates", limits.MaximumCandidates, hardMaximumCandidates},
		{"maximum bucket members", limits.MaximumBucketMembers, hardMaximumBucketMembers},
		{"maximum cluster members", limits.MaximumClusterMembers, hardMaximumClusterMembers},
		{"shingle words", limits.ShingleWords, hardMaximumShingleWords},
	}
	for _, check := range checks {
		if check.value < 1 || check.value > check.limit {
			return Limits{}, fmt.Errorf("%s must be between 1 and %d", check.name, check.limit)
		}
	}
	if math.IsNaN(limits.MinimumJaccard) ||
		limits.MinimumJaccard <= 0 || limits.MinimumJaccard > 1 {
		return Limits{}, errors.New("minimum Jaccard must be greater than 0 and at most 1")
	}

	return limits, nil
}

func validateURL(url string) (string, error) {
	normalized := strings.TrimSpace(url)
	if normalized == "" || len(normalized) > maximumURLBytes {
		return "", fmt.Errorf(
			"%w: URL must contain at most %d bytes",
			ErrInvalidEvidence,
			maximumURLBytes,
		)
	}

	return normalized, nil
}
