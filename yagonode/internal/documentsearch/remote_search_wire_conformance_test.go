package documentsearch

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/httpguard"
	"github.com/D4rk4/yago/yagoproto"
)

// TestRemoteSearchWireResponseIsPeerConsumable drives the real inbound
// /yacy/search.html route the way a current YaCy peer's RemoteSearch would — a
// multi-word query over the RWI store — and feeds the raw wire body back through
// the same peer-response parser the outbound swarm path uses to read other peers.
// It is the interop guard for SWARM-07: it proves our RWI-only endpoint (no Solr)
// emits a response a YaCy-compatible peer can parse, including the per-term
// indexcount/indexabstract keys that drive multi-term index-abstract negotiation.
func TestRemoteSearchWireResponseIsPeerConsumable(t *testing.T) {
	word1, word2 := hashFor("w1"), hashFor("w2")
	mux := http.NewServeMux()
	MountSearch(
		httpguard.NewWireRouter(mux, httpguard.WireGate{
			Guard:   httpguard.NewRequestGuard(4096, time.Second),
			Respond: httpguard.NewWireResponder(searchWireStatus{}),
			Address: httpguard.NewClientAddressResolver(nil),
		}),
		searchIdentity(),
		SearchConfig{
			// u2 carries both terms, so it is the single join across the query.
			Index: fakeScanner{postings: map[yagomodel.Hash][]yagomodel.RWIPosting{
				word1: {postingEntry(word1, "u1", 0, 1), postingEntry(word1, "u2", 0, 1)},
				word2: {postingEntry(word2, "u2", 0, 1), postingEntry(word2, "u3", 0, 1)},
			}},
			Documents:      fakeDirectory{rows: urlRows("u2")},
			MatchesPerTerm: 100,
		},
	)

	req := yagoproto.SearchRequest{
		NetworkName: "freeworld",
		Query:       []yagomodel.Hash{word1, word2},
		Count:       10,
		// Request per-term index abstracts for both terms — the exact multi-term
		// negotiation a YaCy peer's RemoteSearch performs before urls= retrieval.
		Abstracts: yagoproto.SearchAbstracts(string(word1) + string(word2)),
	}
	rec := httptest.NewRecorder()
	httpReq := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		yagoproto.PathSearch+"?"+req.Form().Encode(),
		nil,
	)
	mux.ServeHTTP(rec, httpReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	// A YaCy-style peer parses the body with the same reader we use for peers.
	parsed, err := yagoproto.ParseSearchResponse(mustParseMessage(t, rec.Body.String()))
	if err != nil {
		t.Fatalf("peer parser rejected our response: %v\nbody=%s", err, rec.Body.String())
	}
	if parsed.JoinCount != 1 {
		t.Fatalf("joincount = %d, want 1 (u2 carries both terms)", parsed.JoinCount)
	}
	if parsed.Count != len(parsed.Resources) || parsed.Count == 0 {
		t.Fatalf(
			"count = %d, resources = %d, want equal and non-zero",
			parsed.Count,
			len(parsed.Resources),
		)
	}
	for _, word := range []yagomodel.Hash{word1, word2} {
		if parsed.IndexCount[word] == 0 {
			t.Fatalf("indexcount.%s missing/zero: %+v", word, parsed.IndexCount)
		}
		if len(parsed.IndexAbstract[word]) == 0 {
			t.Fatalf("indexabstract.%s missing: %+v", word, parsed.IndexAbstract)
		}
	}

	// The internal linkcount alias must not leak onto the wire.
	if strings.Contains(rec.Body.String(), yagoproto.FieldLinkCount+"=") {
		t.Fatalf("response exposes internal linkcount field: %s", rec.Body.String())
	}
}

func mustParseMessage(t *testing.T, body string) yagomodel.Message {
	t.Helper()
	msg, err := yagomodel.ParseMessage(body)
	if err != nil {
		t.Fatalf("parse wire body: %v", err)
	}

	return msg
}
