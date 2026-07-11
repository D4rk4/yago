package documentstore

import "testing"

func TestContentClusterAssignmentPersistsWithDocument(t *testing.T) {
	directory, receiver := openDocuments(t)
	want := Document{
		NormalizedURL:     "https://a.example",
		ContentHash:       "hash",
		ClusterID:         "cluster-a",
		RepresentativeURL: "https://canonical.example",
	}
	if _, err := receiver.Receive(t.Context(), []Document{want}); err != nil {
		t.Fatalf("receive cluster assignment: %v", err)
	}
	got, found, err := directory.Document(t.Context(), want.NormalizedURL)
	if err != nil || !found {
		t.Fatalf("stored cluster assignment = %+v, %v, %v", got, found, err)
	}
	if got.ClusterID != want.ClusterID || got.RepresentativeURL != want.RepresentativeURL {
		t.Fatalf("stored cluster assignment = %+v", got)
	}
	raw, err := (documentCodec{}).Encode(want)
	if err != nil {
		t.Fatalf("encode cluster assignment: %v", err)
	}
	decoded, err := (documentCodec{}).Decode(raw)
	if err != nil || decoded.ClusterID != want.ClusterID ||
		decoded.RepresentativeURL != want.RepresentativeURL {
		t.Fatalf("decoded cluster assignment = %+v, %v", decoded, err)
	}
}
