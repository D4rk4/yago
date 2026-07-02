package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yacymodel"
	"github.com/D4rk4/yago/yacyproto"
)

type selfTestRoundTripFunc func(*http.Request) (*http.Response, error)

func (f selfTestRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type selfTestFailingBody struct {
	err error
}

func (b selfTestFailingBody) Read([]byte) (int, error) {
	return 0, b.err
}

func (b selfTestFailingBody) Close() error { return nil }

func TestPublicEndpointSelfTestConfirmsQueryResponse(t *testing.T) {
	var got yacyproto.QueryRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodGet {
			t.Fatalf("method = %q, want GET", req.Method)
		}
		if req.URL.Path != yacyproto.PathQuery {
			t.Fatalf("path = %q, want query path", req.URL.Path)
		}
		parsed, err := yacyproto.ParseQueryRequest(req.Context(), req.URL.Query())
		if err != nil {
			t.Fatalf("parse query: %v", err)
		}
		got = parsed
		_, _ = strings.NewReader(
			yacyproto.QueryResponse{Response: 7}.Encode().Encode(),
		).WriteTo(w)
	}))
	defer server.Close()

	base, err := publicSelfTestURL(
		envFrom(map[string]string{envPublicSelfTestURL: server.URL}),
		defaultPeerAddr,
	)
	if err != nil {
		t.Fatalf("publicSelfTestURL: %v", err)
	}

	self := yacymodel.Hash("AAAAAAAAAAAA")
	probe := newPublicEndpointSelfTest(server.Client(), "freeworld", self, base)
	if !probe.Reachable(context.Background()) {
		t.Fatal("Reachable = false, want true")
	}

	want := yacyproto.QueryRequest{
		NetworkName: "freeworld",
		YouAre:      self,
		Object:      yacyproto.ObjectRWICount,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("query = %#v, want %#v", got, want)
	}
}

func TestPublicEndpointSelfTestRejectsReachabilityFailures(t *testing.T) {
	t.Run("nil base", func(t *testing.T) {
		probe := newPublicEndpointSelfTest(nil, "freeworld", yacymodel.Hash("AAAAAAAAAAAA"), nil)
		if probe.client != http.DefaultClient {
			t.Fatal("nil client did not select default client")
		}
		if probe.Reachable(context.Background()) {
			t.Fatal("Reachable = true, want false")
		}
	})

	t.Run("transport", func(t *testing.T) {
		probe := newPublicEndpointSelfTest(
			&http.Client{
				Transport: selfTestRoundTripFunc(func(*http.Request) (*http.Response, error) {
					return nil, errors.New("transport failed")
				}),
			},
			"freeworld",
			yacymodel.Hash("AAAAAAAAAAAA"),
			mustURL(t, "http://127.0.0.1:8090"),
		)
		if probe.Reachable(context.Background()) {
			t.Fatal("Reachable = true, want false")
		}
	})

	t.Run("request", func(t *testing.T) {
		probe := newPublicEndpointSelfTest(
			&http.Client{
				Transport: selfTestRoundTripFunc(func(*http.Request) (*http.Response, error) {
					t.Fatal("transport called")
					return nil, nil
				}),
			},
			"freeworld",
			yacymodel.Hash("AAAAAAAAAAAA"),
			&url.URL{Scheme: "http", Host: "[::1"},
		)
		if probe.Reachable(context.Background()) {
			t.Fatal("Reachable = true, want false")
		}
	})
}

func TestPublicEndpointSelfTestRejectsResponseFailures(t *testing.T) {
	t.Run("status", func(t *testing.T) {
		probe := publicEndpointSelfTest{}
		resp := &http.Response{
			StatusCode: http.StatusBadGateway,
			Body:       io.NopCloser(strings.NewReader("")),
		}
		if probe.confirm(context.Background(), resp) {
			t.Fatal("confirm = true, want false")
		}
	})

	t.Run("read", func(t *testing.T) {
		probe := publicEndpointSelfTest{}
		resp := &http.Response{
			StatusCode: http.StatusOK,
			Body:       selfTestFailingBody{err: errors.New("read failed")},
		}
		if probe.confirm(context.Background(), resp) {
			t.Fatal("confirm = true, want false")
		}
	})

	t.Run("missing response", func(t *testing.T) {
		probe := publicEndpointSelfTest{}
		resp := &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("bad")),
		}
		if probe.confirm(context.Background(), resp) {
			t.Fatal("confirm = true, want false")
		}
	})

	t.Run("query", func(t *testing.T) {
		probe := publicEndpointSelfTest{}
		resp := &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("uptime=1\r\n")),
		}
		if probe.confirm(context.Background(), resp) {
			t.Fatal("confirm = true, want false")
		}
	})

	t.Run("rejected", func(t *testing.T) {
		probe := publicEndpointSelfTest{}
		resp := &http.Response{
			StatusCode: http.StatusOK,
			Body: io.NopCloser(strings.NewReader(
				yacyproto.QueryResponse{
					Response: yacyproto.QueryResponseRejected,
				}.Encode().
					Encode(),
			)),
		}
		if probe.confirm(context.Background(), resp) {
			t.Fatal("confirm = true, want false")
		}
	})

	t.Run("negative", func(t *testing.T) {
		probe := publicEndpointSelfTest{}
		resp := &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("response=-2\r\n")),
		}
		if probe.confirm(context.Background(), resp) {
			t.Fatal("confirm = true, want false")
		}
	})
}

func TestPublicEndpointSelfTestBuildsQueryURL(t *testing.T) {
	probe := newPublicEndpointSelfTest(
		nil,
		"freeworld",
		yacymodel.Hash("AAAAAAAAAAAA"),
		mustURL(t, "https://peer.example/base/"),
	)

	got := probe.queryURL()

	if got.Scheme != "https" ||
		got.Host != "peer.example" ||
		got.Path != "/base/yacy/query.html" ||
		got.Query().Get(yacyproto.FieldObject) != string(yacyproto.ObjectRWICount) {
		t.Fatalf("query URL = %s", got.String())
	}
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()

	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}

	return parsed
}
