package api

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/internal/core/contracts"
	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
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
		Hash:     testHash(tb, word),
		Name:     yacymodel.Some(name),
		PeerType: yacymodel.Some(yacymodel.PeerSenior),
	}

	roundTrip, err := yacymodel.ParseSeed(tb.Context(), seed.String())
	if err != nil {
		tb.Fatalf("seed does not round-trip: %v", err)
	}

	return roundTrip
}

type fakeStatus struct {
	snapshot contracts.StatusSnapshot
}

func (f fakeStatus) Snapshot(context.Context) contracts.StatusSnapshot { return f.snapshot }

type fakePeers struct {
	outcome contracts.HelloOutcome
	err     error
	caller  yacymodel.Seed
	count   int
	called  bool
}

func (f *fakePeers) Hello(
	_ context.Context,
	caller yacymodel.Seed,
	count int,
) (contracts.HelloOutcome, error) {
	f.called = true
	f.caller = caller
	f.count = count

	return f.outcome, f.err
}

type fakeRWIReceiver struct {
	receipt contracts.RWIReceipt
	err     error
	entries []yacymodel.RWIPosting
	called  bool
}

func (f *fakeRWIReceiver) ReceiveRWI(
	_ context.Context,
	entries []yacymodel.RWIPosting,
) (contracts.RWIReceipt, error) {
	f.called = true
	f.entries = entries

	return f.receipt, f.err
}

type fakeURLReceiver struct {
	receipt contracts.URLReceipt
	err     error
	rows    []yacymodel.URIMetadataRow
	called  bool
}

func (f *fakeURLReceiver) ReceiveURLs(
	_ context.Context,
	rows []yacymodel.URIMetadataRow,
) (contracts.URLReceipt, error) {
	f.called = true
	f.rows = rows

	return f.receipt, f.err
}

type fakeSearcher struct {
	result contracts.SearchResult
	err    error
	query  contracts.SearchQuery
	called bool
}

func (f *fakeSearcher) Search(
	_ context.Context,
	query contracts.SearchQuery,
) (contracts.SearchResult, error) {
	f.called = true
	f.query = query

	return f.result, f.err
}

type fakeCounter struct {
	count  int
	err    error
	kind   contracts.CountKind
	called bool
}

func (f *fakeCounter) Count(_ context.Context, kind contracts.CountKind) (int, error) {
	f.called = true
	f.kind = kind

	return f.count, f.err
}

type testHarness struct {
	ident    yacymodel.PeerIdentity
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
		ident: yacymodel.PeerIdentity{Hash: testHash(tb, "node")},
		status: fakeStatus{snapshot: contracts.StatusSnapshot{
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

func (h *testHarness) guard() RequestGuard {
	return NewRequestGuard(h.ident, DefaultMaxBodyBytes, DefaultRequestTimeout)
}

func (h *testHarness) mux() *http.ServeMux {
	return h.muxWith(h.guard(), nil)
}

func (h *testHarness) muxWith(guard RequestGuard, trustedProxies []*net.IPNet) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("/{$}", NewLandingPageHandler())
	mux.Handle(yacyproto.PathHello, NewHelloHandler(guard, h.status, h.peers, trustedProxies))
	mux.Handle(yacyproto.PathTransferRWI, NewTransferRWIHandler(guard, h.status, h.rwi))
	mux.Handle(yacyproto.PathTransferURL, NewTransferURLHandler(guard, h.status, h.urls))
	mux.Handle(yacyproto.PathSearch, NewSearchHandler(guard, h.status, h.searcher))
	mux.Handle(yacyproto.PathQuery, NewQueryHandler(guard, h.status, h.counter))
	mux.Handle(yacyproto.PathCrawlReceipt, NewCrawlReceiptHandler(guard, h.status))

	return mux
}

func (h *testHarness) do(
	tb testing.TB,
	method, path string,
	form url.Values,
) *httptest.ResponseRecorder {
	tb.Helper()

	return h.doWith(tb, method, path, form, h.mux())
}

func (h *testHarness) doWith(
	tb testing.TB,
	method, path string,
	form url.Values,
	mux *http.ServeMux,
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
	mux.ServeHTTP(rec, req)

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

	msg, err := yacymodel.ParseMessage(rec.Body.String())
	if err != nil {
		tb.Fatalf("parse response: %v", err)
	}

	return msg
}
