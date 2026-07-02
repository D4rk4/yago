package indextransfer

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yacymodel"
	"github.com/D4rk4/yago/yacyproto"
)

func TestNewRemoteRWICountProbeUsesDefaultClient(t *testing.T) {
	t.Parallel()

	probe := NewRemoteRWICountProbe(nil, yacyproto.DefaultNetwork, yacymodel.Seed{})
	if probe.client != http.DefaultClient {
		t.Fatal("nil client did not select http.DefaultClient")
	}
}

func TestRemoteRWICountProbePostsYaCyQueryAndParsesResponse(t *testing.T) {
	t.Parallel()

	self := yacymodel.Seed{Hash: hashOf(t, "self")}
	var got yacyproto.QueryRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != yacyproto.PathQuery {
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
		parsed, err := yacyproto.ParseQueryRequest(req.Context(), req.PostForm)
		if err != nil {
			t.Fatalf("parse query request: %v", err)
		}
		got = parsed
		msg := yacyproto.QueryResponse{Response: 321}.Encode()
		_, _ = strings.NewReader(msg.Encode()).WriteTo(w)
	}))
	defer server.Close()

	peer := serverSeed(t, server)
	count, err := NewRemoteRWICountProbe(server.Client(), yacyproto.DefaultNetwork, self).
		RWICount(context.Background(), peer)
	if err != nil {
		t.Fatalf("RWICount: %v", err)
	}

	want := yacyproto.QueryRequest{
		NetworkName: yacyproto.DefaultNetwork,
		YouAre:      peer.Hash,
		Iam:         self.Hash,
		Object:      yacyproto.ObjectRWICount,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("request = %#v, want %#v", got, want)
	}
	if count != 321 {
		t.Fatalf("count = %d, want 321", count)
	}
}

func TestRemoteRWICountProbeRejectsUnreachablePeer(t *testing.T) {
	t.Parallel()

	probe := NewRemoteRWICountProbe(http.DefaultClient, yacyproto.DefaultNetwork, yacymodel.Seed{})
	if _, err := probe.RWICount(
		context.Background(),
		yacymodel.Seed{Hash: hashOf(t, "peer")},
	); err == nil {
		t.Fatal("expected unreachable peer error")
	}
}

func TestRemoteRWICountProbeRejectsRemoteRejection(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		msg := yacyproto.QueryResponse{Response: yacyproto.QueryResponseRejected}.Encode()
		_, _ = strings.NewReader(msg.Encode()).WriteTo(w)
	}))
	defer server.Close()

	peer := serverSeed(t, server)
	_, err := NewRemoteRWICountProbe(server.Client(), yacyproto.DefaultNetwork, yacymodel.Seed{}).
		RWICount(context.Background(), peer)
	if !errors.Is(err, ErrCapacityProbeRejected) {
		t.Fatalf("error = %v, want ErrCapacityProbeRejected", err)
	}
}

func TestRemoteRWICountProbeRejectsNegativeResponse(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		msg := yacymodel.Message{yacyproto.FieldResponse: "-2"}
		_, _ = strings.NewReader(msg.Encode()).WriteTo(w)
	}))
	defer server.Close()

	peer := serverSeed(t, server)
	if _, err := NewRemoteRWICountProbe(
		server.Client(),
		yacyproto.DefaultNetwork,
		yacymodel.Seed{},
	).
		RWICount(context.Background(), peer); err == nil {
		t.Fatal("expected negative response error")
	}
}

func TestRemoteRWICountProbeWrapsProtocolErrors(t *testing.T) {
	t.Parallel()

	probe := NewRemoteRWICountProbe(
		&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("magic=ok\n")),
			}, nil
		})},
		yacyproto.DefaultNetwork,
		yacymodel.Seed{},
	)
	peer := peerSeed(t)
	if _, err := probe.RWICount(context.Background(), peer); err == nil {
		t.Fatal("expected protocol error")
	}
}
