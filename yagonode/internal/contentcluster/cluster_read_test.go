package contentcluster

import (
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestClusterReturnsBoundedPersistentMembershipCopy(t *testing.T) {
	index, _ := openMemoryIndex(t, Limits{})
	first := replaceEvidence(t, index, Evidence{
		URL:         "https://a.example",
		ContentHash: "same",
		Text:        "short",
	})
	replaceEvidence(t, index, Evidence{
		URL:                "https://b.example",
		ContentHash:        "same",
		Text:               "short",
		CanonicalPreferred: true,
	})
	cluster, found, err := index.Cluster(t.Context(), first.ClusterID)
	if err != nil || !found {
		t.Fatalf("cluster = %+v, %v, %v", cluster, found, err)
	}
	if cluster.ID != first.ClusterID || cluster.RepresentativeURL != "https://b.example" ||
		len(cluster.MemberURLs) != 2 {
		t.Fatalf("cluster = %+v", cluster)
	}
	cluster.MemberURLs[0] = "changed"
	again, found, err := index.Cluster(t.Context(), first.ClusterID)
	if err != nil || !found || again.MemberURLs[0] == "changed" {
		t.Fatalf("cluster copy = %+v, %v, %v", again, found, err)
	}
	if _, found, err := index.Cluster(t.Context(), "missing"); err != nil || found {
		t.Fatalf("missing cluster = %v, %v", found, err)
	}
	if _, _, err := index.Cluster(t.Context(), " "); !errors.Is(err, ErrInvalidEvidence) {
		t.Fatalf("invalid cluster error = %v", err)
	}
}

func TestClusterReportsCorruptAndOversizedRecords(t *testing.T) {
	index, engine := openFaultIndex(t, Limits{})
	engine.putRaw(clusterBucketName, vault.Key("broken"), []byte("{"))
	if _, _, err := index.Cluster(t.Context(), "broken"); err == nil {
		t.Fatal("corrupt cluster record succeeded")
	}
	limits := DefaultLimits()
	limits.MaximumClusterMembers = 1
	bounded, boundedEngine := openFaultIndex(t, limits)
	raw, err := (jsonCodec[clusterRecord]{}).Encode(clusterRecord{
		ID:      "large",
		Members: []string{"a", "b"},
	})
	if err != nil {
		t.Fatalf("encode oversized cluster: %v", err)
	}
	boundedEngine.putRaw(clusterBucketName, vault.Key("large"), raw)
	if _, _, err := bounded.Cluster(t.Context(), "large"); err == nil {
		t.Fatal("oversized cluster record succeeded")
	}
}
