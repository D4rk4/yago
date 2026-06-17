package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/internal/core"
	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

func testHash(tb testing.TB, word string) yacymodel.Hash {
	tb.Helper()

	hash := yacymodel.WordHash(word)
	if !hash.Valid() {
		tb.Fatalf("hash for %q invalid: %q", word, hash)
	}

	return hash
}

func testSeed(tb testing.TB, word, name string) yacymodel.Seed {
	tb.Helper()

	seed := yacymodel.Seed{
		yacymodel.SeedHash:     testHash(tb, word).String(),
		yacymodel.SeedName:     name,
		yacymodel.SeedPeerType: yacymodel.PeerSenior.String(),
	}

	roundTrip, err := yacymodel.ParseSeed(seed.String())
	if err != nil {
		tb.Fatalf("seed does not round-trip: %v", err)
	}

	return roundTrip
}

type fakeIdentity struct {
	hash    yacymodel.Hash
	network string
}

func (f fakeIdentity) Hash() yacymodel.Hash { return f.hash }
func (f fakeIdentity) NetworkName() string  { return f.network }

type fakeStatus struct {
	snapshot core.StatusSnapshot
}

func (f fakeStatus) Snapshot(context.Context) core.StatusSnapshot { return f.snapshot }

type fakePeers struct {
	outcome core.HelloOutcome
	err     error
	caller  yacymodel.Seed
	called  bool
}

func (f *fakePeers) Hello(_ context.Context, caller yacymodel.Seed) (core.HelloOutcome, error) {
	f.called = true
	f.caller = caller

	return f.outcome, f.err
}

type fakeRWIReceiver struct {
	receipt core.RWIReceipt
	err     error
	entries []yacymodel.RWIEntry
	called  bool
}

func (f *fakeRWIReceiver) ReceiveRWI(
	_ context.Context,
	entries []yacymodel.RWIEntry,
) (core.RWIReceipt, error) {
	f.called = true
	f.entries = entries

	return f.receipt, f.err
}

type fakeURLReceiver struct {
	receipt core.URLReceipt
	err     error
	rows    []yacymodel.URIMetadataRow
	called  bool
}

func (f *fakeURLReceiver) ReceiveURLs(
	_ context.Context,
	rows []yacymodel.URIMetadataRow,
) (core.URLReceipt, error) {
	f.called = true
	f.rows = rows

	return f.receipt, f.err
}

type fakeSearcher struct {
	result core.SearchResult
	err    error
	query  core.SearchQuery
	called bool
}

func (f *fakeSearcher) Search(
	_ context.Context,
	query core.SearchQuery,
) (core.SearchResult, error) {
	f.called = true
	f.query = query

	return f.result, f.err
}

type fakeCounter struct {
	count  int
	err    error
	kind   core.CountKind
	called bool
}

func (f *fakeCounter) Count(_ context.Context, kind core.CountKind) (int, error) {
	f.called = true
	f.kind = kind

	return f.count, f.err
}

type testHarness struct {
	ident    fakeIdentity
	status   fakeStatus
	peers    *fakePeers
	rwi      *fakeRWIReceiver
	urls     *fakeURLReceiver
	searcher *fakeSearcher
	counter  *fakeCounter
}

func newTestHarness(tb testing.TB) *testHarness {
	tb.Helper()

	return &testHarness{
		ident: fakeIdentity{hash: testHash(tb, "node"), network: ""},
		status: fakeStatus{snapshot: core.StatusSnapshot{
			Version: "1.0",
			Uptime:  7,
			Seed:    testSeed(tb, "node", "self"),
		}},
		peers:    &fakePeers{},
		rwi:      &fakeRWIReceiver{},
		urls:     &fakeURLReceiver{},
		searcher: &fakeSearcher{},
		counter:  &fakeCounter{},
	}
}

func (h *testHarness) mux(opts ...Option) *http.ServeMux {
	return NewPeerProtocolMux(
		h.ident, h.status, h.peers, h.rwi, h.urls, h.searcher, h.counter, opts...,
	)
}

func (h *testHarness) do(
	tb testing.TB,
	method, path string,
	form url.Values,
	opts ...Option,
) *httptest.ResponseRecorder {
	tb.Helper()

	var body string
	if form != nil {
		body = form.Encode()
	}

	req := httptest.NewRequestWithContext(tb.Context(), method, path, strings.NewReader(body))
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}

	rec := httptest.NewRecorder()
	h.mux(opts...).ServeHTTP(rec, req)

	return rec
}

func newRecorder() *httptest.ResponseRecorder { return httptest.NewRecorder() }

func httptestForwarded(
	tb testing.TB,
	form url.Values,
	path, remoteAddr, forwarded string,
) *http.Request {
	tb.Helper()

	r := httptest.NewRequestWithContext(
		tb.Context(),
		http.MethodPost,
		path,
		strings.NewReader(form.Encode()),
	)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.RemoteAddr = remoteAddr
	r.Header.Set(forwardedForHeader, forwarded)

	return r
}

func decodeResponse(tb testing.TB, rec *httptest.ResponseRecorder) yacymodel.Message {
	tb.Helper()

	if rec.Code != http.StatusOK {
		tb.Fatalf("status = %d, want 200; body %q", rec.Code, rec.Body.String())
	}

	return yacymodel.ParseMessage(rec.Body.String())
}
