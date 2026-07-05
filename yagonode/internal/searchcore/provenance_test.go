package searchcore

import "testing"

func TestResultProvenanceClassification(t *testing.T) {
	cases := map[string]struct {
		source Source
		peer   bool
		web    bool
		local  bool
	}{
		"remote": {source: SourceRemote, peer: true},
		"web":    {source: SourceWeb, web: true},
		"local":  {source: SourceLocal, local: true},
		// A local hit served inside a global search carries the request source;
		// it is still this node's stored page.
		"global": {source: SourceGlobal, local: true},
	}
	for name, tc := range cases {
		result := Result{Source: tc.source}
		if result.FromPeer() != tc.peer ||
			result.FromWeb() != tc.web ||
			result.StoredLocally() != tc.local {
			t.Fatalf("%s: peer=%v web=%v local=%v, want %+v",
				name, result.FromPeer(), result.FromWeb(), result.StoredLocally(), tc)
		}
	}
}
