package indextransfer

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagoproto"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type failingBody struct {
	err error
}

func (b failingBody) Read([]byte) (int, error) {
	return 0, b.err
}

func (b failingBody) Close() error {
	return nil
}

func hashOf(tb testing.TB, word string) yagomodel.Hash {
	tb.Helper()

	hash := yagomodel.WordHash(word)
	if !hash.Valid() {
		tb.Fatalf("invalid hash for %q: %q", word, hash)
	}

	return hash
}

func peerSeed(tb testing.TB) yagomodel.Seed {
	tb.Helper()

	host, err := yagomodel.ParseHost("192.0.2.1")
	if err != nil {
		tb.Fatalf("parse host: %v", err)
	}

	return yagomodel.Seed{
		Hash: hashOf(tb, "peer"),
		IP:   yagomodel.Some(host),
		Port: yagomodel.Some(yagomodel.Port(8090)),
	}
}

func serverSeed(tb testing.TB, server *httptest.Server) yagomodel.Seed {
	tb.Helper()

	parsed, err := url.Parse(server.URL)
	if err != nil {
		tb.Fatalf("parse server url: %v", err)
	}
	rawHost, rawPort, err := net.SplitHostPort(parsed.Host)
	if err != nil {
		tb.Fatalf("split server address: %v", err)
	}
	host, err := yagomodel.ParseHost(rawHost)
	if err != nil {
		tb.Fatalf("parse server host: %v", err)
	}
	port, err := yagomodel.ParsePort(rawPort)
	if err != nil {
		tb.Fatalf("parse server port: %v", err)
	}

	return yagomodel.Seed{
		Hash: hashOf(tb, "peer"),
		IP:   yagomodel.Some(host),
		Port: yagomodel.Some(port),
	}
}

func postingOf(tb testing.TB, word, urlWord string) yagomodel.RWIPosting {
	tb.Helper()

	posting := yagomodel.RWIPosting{
		WordHash: hashOf(tb, word),
		Properties: map[string]string{
			yagomodel.ColURLHash:        hashOf(tb, urlWord).String(),
			yagomodel.ColLocalLinkCount: "AB",
		},
	}
	roundTrip, err := yagomodel.ParseRWIPosting(posting.String())
	if err != nil {
		tb.Fatalf("parse posting: %v", err)
	}

	return roundTrip
}

func rowOf(tb testing.TB, urlWord string) yagomodel.URIMetadataRow {
	tb.Helper()

	row := yagomodel.URIMetadataRow{
		Properties: map[string]string{
			yagomodel.URLMetaHash: hashOf(tb, urlWord).String(),
		},
	}
	roundTrip, err := yagomodel.ParseURIMetadataRow(row.String())
	if err != nil {
		tb.Fatalf("parse row: %v", err)
	}

	return roundTrip
}

func TestNewHTTPPeerWriterUsesDefaultClient(t *testing.T) {
	t.Parallel()

	writer := NewHTTPPeerWriter(nil, yagoproto.DefaultNetwork, yagomodel.Seed{})
	if writer.client != http.DefaultClient {
		t.Fatal("nil client did not select http.DefaultClient")
	}
}

func TestTransferRWIHandlesEmptyPostingBatchLocally(t *testing.T) {
	t.Parallel()

	writer := NewHTTPPeerWriter(
		&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			t.Fatal("empty posting batch should not perform HTTP")

			return nil, nil
		})},
		yagoproto.DefaultNetwork,
		yagomodel.Seed{Hash: hashOf(t, "self")},
	)

	resp, err := writer.TransferRWI(context.Background(), yagomodel.Seed{}, nil)
	if err != nil {
		t.Fatalf("TransferRWI: %v", err)
	}
	if resp.Result != yagoproto.ResultOK {
		t.Fatalf("Result = %q, want ok", resp.Result)
	}
}

func TestTransferRWIPostsYaCyFormAndParsesResponse(t *testing.T) {
	t.Parallel()

	self := yagomodel.Seed{Hash: hashOf(t, "self")}
	postings := []yagomodel.RWIPosting{
		postingOf(t, "word-a", "url-a"),
		postingOf(t, "word-a", "url-b"),
		postingOf(t, "word-b", "url-c"),
	}
	unknown := hashOf(t, "url-a")
	var got yagoproto.TransferRWIRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != yagoproto.PathTransferRWI {
			t.Fatalf("path = %q", req.URL.Path)
		}
		if req.Method != http.MethodPost {
			t.Fatalf("method = %q", req.Method)
		}
		if ct := req.Header.Get("Content-Type"); ct != "application/x-www-form-urlencoded" {
			t.Fatalf("content type = %q", ct)
		}
		if err := req.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		parsed, err := yagoproto.ParseTransferRWIRequest(req.Context(), req.PostForm)
		if err != nil {
			t.Fatalf("parse transfer rwi request: %v", err)
		}
		got = parsed
		msg := yagoproto.TransferRWIResponse{
			Result:     yagoproto.ResultOK,
			Pause:      3,
			UnknownURL: []yagomodel.Hash{unknown},
		}.Encode()
		_, _ = strings.NewReader(msg.Encode()).WriteTo(w)
	}))
	defer server.Close()

	peer := serverSeed(t, server)
	resp, err := NewHTTPPeerWriter(server.Client(), yagoproto.DefaultNetwork, self).
		TransferRWI(context.Background(), peer, postings)
	if err != nil {
		t.Fatalf("TransferRWI: %v", err)
	}

	if got.NetworkName != yagoproto.DefaultNetwork {
		t.Fatalf("network = %q", got.NetworkName)
	}
	if got.Iam != self.Hash || got.YouAre != peer.Hash {
		t.Fatalf("identity fields = %q/%q", got.Iam, got.YouAre)
	}
	if got.WordCount != 2 || got.EntryCount != 3 {
		t.Fatalf("counts = %d/%d, want 2/3", got.WordCount, got.EntryCount)
	}
	if !reflect.DeepEqual(got.Indexes, postings) {
		t.Fatalf("postings mismatch:\n got %#v\nwant %#v", got.Indexes, postings)
	}
	if resp.Result != yagoproto.ResultOK || resp.Pause != 3 {
		t.Fatalf("response = %#v", resp)
	}
	if !reflect.DeepEqual(resp.UnknownURL, []yagomodel.Hash{unknown}) {
		t.Fatalf("unknown = %#v", resp.UnknownURL)
	}
}

func TestTransferURLPostsYaCyFormAndParsesResponse(t *testing.T) {
	t.Parallel()

	self := yagomodel.Seed{Hash: hashOf(t, "self")}
	rows := []yagomodel.URIMetadataRow{rowOf(t, "url-a"), rowOf(t, "url-b")}
	rejected := hashOf(t, "url-b")
	var got yagoproto.TransferURLRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != yagoproto.PathTransferURL {
			t.Fatalf("path = %q", req.URL.Path)
		}
		if err := req.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		parsed, err := yagoproto.ParseTransferURLRequest(req.Context(), req.PostForm)
		if err != nil {
			t.Fatalf("parse transfer url request: %v", err)
		}
		got = parsed
		msg := yagoproto.TransferURLResponse{
			Result:   yagoproto.TransferURLResult(yagoproto.ResultOK),
			Double:   1,
			ErrorURL: []yagomodel.Hash{rejected},
		}.Encode()
		_, _ = strings.NewReader(msg.Encode()).WriteTo(w)
	}))
	defer server.Close()

	peer := serverSeed(t, server)
	resp, err := NewHTTPPeerWriter(server.Client(), yagoproto.DefaultNetwork, self).
		TransferURL(context.Background(), peer, rows)
	if err != nil {
		t.Fatalf("TransferURL: %v", err)
	}

	if got.NetworkName != yagoproto.DefaultNetwork {
		t.Fatalf("network = %q", got.NetworkName)
	}
	if got.Iam != self.Hash || got.YouAre != peer.Hash {
		t.Fatalf("identity fields = %q/%q", got.Iam, got.YouAre)
	}
	if !reflect.DeepEqual(got.URLs, rows) {
		t.Fatalf("rows mismatch:\n got %#v\nwant %#v", got.URLs, rows)
	}
	if got.URLCount != 2 || resp.Result != yagoproto.TransferURLResult(yagoproto.ResultOK) ||
		resp.Double != 1 {
		t.Fatalf("transfer result = request %#v response %#v", got, resp)
	}
	if !reflect.DeepEqual(resp.ErrorURL, []yagomodel.Hash{rejected}) {
		t.Fatalf("errorURL = %#v", resp.ErrorURL)
	}
}

func TestTransferRejectsUnreachablePeer(t *testing.T) {
	t.Parallel()

	writer := NewHTTPPeerWriter(http.DefaultClient, yagoproto.DefaultNetwork, yagomodel.Seed{})
	if _, err := writer.TransferURL(
		context.Background(),
		yagomodel.Seed{Hash: hashOf(t, "peer")},
		nil,
	); err == nil {
		t.Fatal("expected unreachable peer error")
	}
}

func TestTransferRejectsRequestCreationError(t *testing.T) {
	saved := newTransferRequest
	t.Cleanup(func() { newTransferRequest = saved })
	newTransferRequest = func(context.Context, string, string, io.Reader) (*http.Request, error) {
		return nil, errors.New("request boom")
	}

	writer := NewHTTPPeerWriter(http.DefaultClient, yagoproto.DefaultNetwork, yagomodel.Seed{})
	peer := peerSeed(t)
	if _, err := writer.TransferURL(context.Background(), peer, nil); err == nil {
		t.Fatal("expected request creation error")
	}
}

func TestTransferRejectsPostError(t *testing.T) {
	t.Parallel()

	writer := NewHTTPPeerWriter(
		&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("post boom")
		})},
		yagoproto.DefaultNetwork,
		yagomodel.Seed{},
	)
	peer := peerSeed(t)
	if _, err := writer.TransferURL(context.Background(), peer, nil); err == nil {
		t.Fatal("expected post error")
	}
}

func TestTransferRejectsNonOKStatus(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "busy", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	peer := serverSeed(t, server)
	writer := NewHTTPPeerWriter(server.Client(), yagoproto.DefaultNetwork, yagomodel.Seed{})
	if _, err := writer.TransferURL(context.Background(), peer, nil); err == nil {
		t.Fatal("expected status error")
	}
}

func TestTransferRejectsReadError(t *testing.T) {
	t.Parallel()

	writer := NewHTTPPeerWriter(
		&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       failingBody{err: errors.New("read boom")},
			}, nil
		})},
		yagoproto.DefaultNetwork,
		yagomodel.Seed{},
	)
	peer := peerSeed(t)
	if _, err := writer.TransferURL(context.Background(), peer, nil); err == nil {
		t.Fatal("expected read error")
	}
}

func TestTransferRejectsOversizedResponse(t *testing.T) {
	t.Parallel()

	writer := NewHTTPPeerWriter(
		&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(
					strings.NewReader(strings.Repeat("x", int(transferMaxBodyBytes)+1)),
				),
			}, nil
		})},
		yagoproto.DefaultNetwork,
		yagomodel.Seed{},
	)
	peer := peerSeed(t)
	if _, err := writer.TransferURL(context.Background(), peer, nil); err == nil {
		t.Fatal("expected oversized response error")
	}
}

func TestTransferRejectsMessageParseError(t *testing.T) {
	saved := parseTransferMessage
	t.Cleanup(func() { parseTransferMessage = saved })
	parseTransferMessage = func(string) (yagomodel.Message, error) {
		return nil, errors.New("parse boom")
	}

	writer := NewHTTPPeerWriter(
		&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("result=ok\n")),
			}, nil
		})},
		yagoproto.DefaultNetwork,
		yagomodel.Seed{},
	)
	peer := peerSeed(t)
	if _, err := writer.TransferURL(context.Background(), peer, nil); err == nil {
		t.Fatal("expected message parse error")
	}
}

func TestTransferRejectsProtocolResponseError(t *testing.T) {
	t.Parallel()

	writer := NewHTTPPeerWriter(
		&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("result=later\n")),
			}, nil
		})},
		yagoproto.DefaultNetwork,
		yagomodel.Seed{},
	)
	peer := peerSeed(t)
	if _, err := writer.TransferURL(context.Background(), peer, nil); err == nil {
		t.Fatal("expected protocol response error")
	}
}
