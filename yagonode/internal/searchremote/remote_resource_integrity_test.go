package searchremote

import (
	"testing"

	"github.com/D4rk4/yago/yagoproto"
)

func TestRemoteResourceIntegrityAcceptsValidPartialRows(t *testing.T) {
	response := yagoproto.SearchResponse{Count: 2, Resources: nil}
	if err := validateRemoteResourceIntegrity(response); err != nil {
		t.Fatal(err)
	}
}

func TestRemoteResourceIntegrityRejectsInvalidEvidence(t *testing.T) {
	for _, test := range []struct {
		response yagoproto.SearchResponse
	}{
		{yagoproto.SearchResponse{Count: -1}},
		{yagoproto.SearchResponse{InvalidResources: 1}},
	} {
		if err := validateRemoteResourceIntegrity(test.response); err == nil {
			t.Fatalf("accepted %#v", test.response)
		}
	}
}
