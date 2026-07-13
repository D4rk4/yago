package searchcore

import "testing"

func TestResultDisplayDateCanonicalForm(t *testing.T) {
	if got := (Result{Date: "20260520"}).DisplayDate(); got != "Wed, 20 May 2026" {
		t.Fatalf("display date = %q, want canonical form", got)
	}
	for _, raw := range []string{"", "00010101", "00010102", "not-a-date", "2026052"} {
		if got := (Result{Date: raw}).DisplayDate(); got != "" {
			t.Fatalf("display date for %q = %q, want empty", raw, got)
		}
	}
}

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
