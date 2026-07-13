package searchcore

import "testing"

func TestPartialFailureSourceLabel(t *testing.T) {
	web := PartialFailure{Source: PartialFailureSourceWeb}
	if web.SourceLabel() != "web" {
		t.Fatalf("web source label = %q", web.SourceLabel())
	}
	peer := PartialFailure{Source: PartialFailureSourceRemoteYaCy}
	if peer.SourceLabel() != PartialFailureSourceRemoteYaCy {
		t.Fatalf("peer source label = %q", peer.SourceLabel())
	}
}
