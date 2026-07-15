package contentcluster

import (
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestReplaceBatchCommitsFourAmortizedPhases(t *testing.T) {
	index, engine := openFaultIndex(t, Limits{})
	before := engine.updates
	evidence := []Evidence{
		{URL: "https://one.example", ContentHash: "shared", Text: "one"},
		{URL: "https://two.example", ContentHash: "shared", Text: "two"},
		{URL: "https://three.example", ContentHash: "other", Text: "three"},
	}
	replacements, err := index.ReplaceBatch(t.Context(), evidence)
	if err != nil {
		t.Fatal(err)
	}
	if engine.updates-before != 4 {
		t.Fatalf("transactions = %d, want 4", engine.updates-before)
	}
	if len(replacements) != len(evidence) {
		t.Fatalf("replacements = %d, want %d", len(replacements), len(evidence))
	}
	if replacements[0].PreviousFound || replacements[1].PreviousFound ||
		replacements[2].PreviousFound {
		t.Fatalf("unexpected previous assignments: %#v", replacements)
	}
	if replacements[0].Current.ClusterID != replacements[1].Current.ClusterID ||
		replacements[2].Current.ClusterID == replacements[0].Current.ClusterID {
		t.Fatalf("assignments = %#v", replacements)
	}
}

func TestReplaceBatchReportsPreviousAssignment(t *testing.T) {
	index, _ := openMemoryIndex(t, Limits{})
	first := replaceEvidence(t, index, Evidence{
		URL: "https://example.org", ContentHash: "before", Text: "before",
	})
	replacements, err := index.ReplaceBatch(t.Context(), []Evidence{{
		URL: "https://example.org", ContentHash: "after", Text: "after",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(replacements) != 1 || !replacements[0].PreviousFound ||
		replacements[0].Previous != first ||
		replacements[0].Current.ClusterID == first.ClusterID {
		t.Fatalf("replacement = %#v", replacements)
	}
}

func TestReplaceBatchValidatesBeforeWriting(t *testing.T) {
	index, engine := openFaultIndex(t, Limits{})
	before := engine.updates
	_, err := index.ReplaceBatch(t.Context(), []Evidence{
		{URL: "https://valid.example", ContentHash: "valid", Text: "valid"},
		{URL: "", ContentHash: "invalid", Text: "invalid"},
	})
	if err == nil {
		t.Fatal("expected invalid evidence")
	}
	if engine.updates != before {
		t.Fatalf("transactions = %d, want 0", engine.updates-before)
	}
}

func TestReplaceBatchReportsCorruptPreviousFingerprint(t *testing.T) {
	index, engine := openFaultIndex(t, Limits{})
	url := "https://example.org/corrupt-fingerprint"
	engine.putRaw(fingerprintBucketName, vault.Key(url), []byte("{"))
	if _, err := index.ReplaceBatch(t.Context(), []Evidence{{
		URL: url, ContentHash: "after", Text: "after",
	}}); err == nil {
		t.Fatal("corrupt previous fingerprint was accepted")
	}
}

func TestReplaceBatchReportsCorruptPreviousCluster(t *testing.T) {
	index, engine := openFaultIndex(t, Limits{})
	evidence := Evidence{
		URL: "https://example.org/corrupt-cluster", ContentHash: "before", Text: "before",
	}
	assignment := replaceEvidence(t, index, evidence)
	engine.putRaw(clusterBucketName, vault.Key(assignment.ClusterID), []byte("{"))
	evidence.ContentHash = "after"
	evidence.Text = "after"
	if _, err := index.ReplaceBatch(t.Context(), []Evidence{evidence}); err == nil {
		t.Fatal("corrupt previous cluster was accepted")
	}
}

func TestReplaceBatchReportsReplacementWriteFailure(t *testing.T) {
	index, engine := openFaultIndex(t, Limits{})
	engine.putFailure = fingerprintBucketName
	if _, err := index.ReplaceBatch(t.Context(), []Evidence{{
		URL: "https://example.org/write-failure", ContentHash: "hash", Text: "text",
	}}); err == nil {
		t.Fatal("replacement write failure was not reported")
	}
}
