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

	"github.com/D4rk4/yago/yacymodel"
	"github.com/D4rk4/yago/yacyproto"
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

func hashOf(tb testing.TB, word string) yacymodel.Hash {
	tb.Helper()

	hash := yacymodel.WordHash(word)
	if !hash.Valid() {
		tb.Fatalf("invalid hash for %q: %q", word, hash)
	}

	return hash
}

func peerSeed(tb testing.TB) yacymodel.Seed {
	tb.Helper()

	host, err := yacymodel.ParseHost("192.0.2.1")
	if err != nil {
		tb.Fatalf("parse host: %v", err)
	}

	return yacymodel.Seed{
		Hash: hashOf(tb, "peer"),
		IP:   yacymodel.Some(host),
		Port: yacymodel.Some(yacymodel.Port(8090)),
	}
}

func serverSeed(tb testing.TB, word string, server *httptest.Server) yacymodel.Seed {
	tb.Helper()

	parsed, err := url.Parse(server.URL)
	if err != nil {
		tb.Fatalf("parse server url: %v", err)
	}
	rawHost, rawPort, err := net.SplitHostPort(parsed.Host)
	if err != nil {
		tb.Fatalf("split server address: %v", err)
	}
	host, err := yacymodel.ParseHost(rawHost)
	if err != nil {
		tb.Fatalf("parse server host: %v", err)
	}
	port, err := yacymodel.ParsePort(rawPort)
	if err != nil {
		tb.Fatalf("parse server port: %v", err)
	}

	return yacymodel.Seed{
		Hash: hashOf(tb, word),
		IP:   yacymodel.Some(host),
		Port: yacymodel.Some(port),
	}
}

func postingOf(tb testing.TB, word, urlWord string) yacymodel.RWIPosting {
	tb.Helper()

	posting := yacymodel.RWIPosting{
		WordHash: hashOf(tb, word),
		Properties: map[string]string{
			yacymodel.ColURLHash:        hashOf(tb, urlWord).String(),
			yacymodel.ColLocalLinkCount: "AB",
		},
	}
	roundTrip, err := yacymodel.ParseRWIPosting(posting.String())
	if err != nil {
		tb.Fatalf("parse posting: %v", err)
	}

	return roundTrip
}

func rowOf(tb testing.TB, urlWord string) yacymodel.URIMetadataRow {
	tb.Helper()

	row := yacymodel.URIMetadataRow{
		Properties: map[string]string{
			yacymodel.URLMetaHash: hashOf(tb, urlWord).String(),
		},
	}
	roundTrip, err := yacymodel.ParseURIMetadataRow(row.String())
	if err != nil {
		tb.Fatalf("parse row: %v", err)
	}

	return roundTrip
}

func TestNewHTTPPeerWriterUsesDefaultClient(t *testing.T) {
	t.Parallel()

	writer := NewHTTPPeerWriter(nil, yacyproto.DefaultNetwork, yacymodel.Seed{})
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
		yacyproto.DefaultNetwork,
		yacymodel.Seed{Hash: hashOf(t, "self")},
	)

	resp, err := writer.TransferRWI(context.Background(), yacymodel.Seed{}, nil)
	if err != nil {
		t.Fatalf("TransferRWI: %v", err)
	}
	if resp.Result != yacyproto.ResultOK {
		t.Fatalf("Result = %q, want ok", resp.Result)
	}
}

func TestTransferRWIPostsYaCyFormAndParsesResponse(t *testing.T) {
	t.Parallel()

	self := yacymodel.Seed{Hash: hashOf(t, "self")}
	postings := []yacymodel.RWIPosting{
		postingOf(t, "word-a", "url-a"),
		postingOf(t, "word-a", "url-b"),
		postingOf(t, "word-b", "url-c"),
	}
	unknown := hashOf(t, "url-a")
	var got yacyproto.TransferRWIRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != yacyproto.PathTransferRWI {
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
		parsed, err := yacyproto.ParseTransferRWIRequest(req.Context(), req.PostForm)
		if err != nil {
			t.Fatalf("parse transfer rwi request: %v", err)
		}
		got = parsed
		msg := yacyproto.TransferRWIResponse{
			Result:     yacyproto.ResultOK,
			Pause:      3,
			UnknownURL: []yacymodel.Hash{unknown},
		}.Encode()
		_, _ = strings.NewReader(msg.Encode()).WriteTo(w)
	}))
	defer server.Close()

	peer := serverSeed(t, "peer", server)
	resp, err := NewHTTPPeerWriter(server.Client(), yacyproto.DefaultNetwork, self).
		TransferRWI(context.Background(), peer, postings)
	if err != nil {
		t.Fatalf("TransferRWI: %v", err)
	}

	if got.NetworkName != yacyproto.DefaultNetwork {
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
	if resp.Result != yacyproto.ResultOK || resp.Pause != 3 {
		t.Fatalf("response = %#v", resp)
	}
	if !reflect.DeepEqual(resp.UnknownURL, []yacymodel.Hash{unknown}) {
		t.Fatalf("unknown = %#v", resp.UnknownURL)
	}
}

func TestTransferURLPostsYaCyFormAndParsesResponse(t *testing.T) {
	t.Parallel()

	self := yacymodel.Seed{Hash: hashOf(t, "self")}
	rows := []yacymodel.URIMetadataRow{rowOf(t, "url-a"), rowOf(t, "url-b")}
	rejected := hashOf(t, "url-b")
	var got yacyproto.TransferURLRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != yacyproto.PathTransferURL {
			t.Fatalf("path = %q", req.URL.Path)
		}
		if err := req.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		parsed, err := yacyproto.ParseTransferURLRequest(req.Context(), req.PostForm)
		if err != nil {
			t.Fatalf("parse transfer url request: %v", err)
		}
		got = parsed
		msg := yacyproto.TransferURLResponse{
			Result:   yacyproto.TransferURLResult(yacyproto.ResultOK),
			Double:   1,
			ErrorURL: []yacymodel.Hash{rejected},
		}.Encode()
		_, _ = strings.NewReader(msg.Encode()).WriteTo(w)
	}))
	defer server.Close()

	peer := serverSeed(t, "peer", server)
	resp, err := NewHTTPPeerWriter(server.Client(), yacyproto.DefaultNetwork, self).
		TransferURL(context.Background(), peer, rows)
	if err != nil {
		t.Fatalf("TransferURL: %v", err)
	}

	if got.NetworkName != yacyproto.DefaultNetwork {
		t.Fatalf("network = %q", got.NetworkName)
	}
	if got.Iam != self.Hash || got.YouAre != peer.Hash {
		t.Fatalf("identity fields = %q/%q", got.Iam, got.YouAre)
	}
	if !reflect.DeepEqual(got.URLs, rows) {
		t.Fatalf("rows mismatch:\n got %#v\nwant %#v", got.URLs, rows)
	}
	if got.URLCount != 2 || resp.Result != yacyproto.TransferURLResult(yacyproto.ResultOK) ||
		resp.Double != 1 {
		t.Fatalf("transfer result = request %#v response %#v", got, resp)
	}
	if !reflect.DeepEqual(resp.ErrorURL, []yacymodel.Hash{rejected}) {
		t.Fatalf("errorURL = %#v", resp.ErrorURL)
	}
}

func TestTransferRejectsUnreachablePeer(t *testing.T) {
	t.Parallel()

	writer := NewHTTPPeerWriter(http.DefaultClient, yacyproto.DefaultNetwork, yacymodel.Seed{})
	if _, err := writer.TransferURL(
		context.Background(),
		yacymodel.Seed{Hash: hashOf(t, "peer")},
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

	writer := NewHTTPPeerWriter(http.DefaultClient, yacyproto.DefaultNetwork, yacymodel.Seed{})
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
		yacyproto.DefaultNetwork,
		yacymodel.Seed{},
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

	peer := serverSeed(t, "peer", server)
	writer := NewHTTPPeerWriter(server.Client(), yacyproto.DefaultNetwork, yacymodel.Seed{})
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
		yacyproto.DefaultNetwork,
		yacymodel.Seed{},
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
		yacyproto.DefaultNetwork,
		yacymodel.Seed{},
	)
	peer := peerSeed(t)
	if _, err := writer.TransferURL(context.Background(), peer, nil); err == nil {
		t.Fatal("expected oversized response error")
	}
}

func TestTransferRejectsMessageParseError(t *testing.T) {
	saved := parseTransferMessage
	t.Cleanup(func() { parseTransferMessage = saved })
	parseTransferMessage = func(string) (yacymodel.Message, error) {
		return nil, errors.New("parse boom")
	}

	writer := NewHTTPPeerWriter(
		&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("result=ok\n")),
			}, nil
		})},
		yacyproto.DefaultNetwork,
		yacymodel.Seed{},
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
		yacyproto.DefaultNetwork,
		yacymodel.Seed{},
	)
	peer := peerSeed(t)
	if _, err := writer.TransferURL(context.Background(), peer, nil); err == nil {
		t.Fatal("expected protocol response error")
	}
}
