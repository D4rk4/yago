package indextransfer

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

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagoproto"
)

func TestNewRemoteRWICountProbeUsesDefaultClient(t *testing.T) {
	t.Parallel()

	probe := NewRemoteRWICountProbe(nil, yagoproto.DefaultNetwork, yagomodel.Seed{}, false)
	if probe.client != http.DefaultClient {
		t.Fatal("nil client did not select http.DefaultClient")
	}
}

func TestRemoteRWICountProbePostsYaCyQueryAndParsesResponse(t *testing.T) {
	t.Parallel()

	self := yagomodel.Seed{Hash: hashOf(t, "self")}
	var got yagoproto.QueryRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != yagoproto.PathQuery {
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
		parsed, err := yagoproto.ParseQueryRequest(req.Context(), req.PostForm)
		if err != nil {
			t.Fatalf("parse query request: %v", err)
		}
		got = parsed
		msg := yagoproto.QueryResponse{Response: 321}.Encode()
		_, _ = strings.NewReader(msg.Encode()).WriteTo(w)
	}))
	defer server.Close()

	peer := serverSeed(t, server)
	count, err := NewRemoteRWICountProbe(server.Client(), yagoproto.DefaultNetwork, self, false).
		RWICount(context.Background(), peer)
	if err != nil {
		t.Fatalf("RWICount: %v", err)
	}

	want := yagoproto.QueryRequest{
		NetworkName: yagoproto.DefaultNetwork,
		YouAre:      peer.Hash,
		Iam:         self.Hash,
		Object:      yagoproto.ObjectRWICount,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("request = %#v, want %#v", got, want)
	}
	if count != 321 {
		t.Fatalf("count = %d, want 321", count)
	}
}

func TestRemoteRWICountProbeSignsControlledNetworkRequest(t *testing.T) {
	self := yagomodel.Seed{Hash: hashOf(t, "self")}
	access := yagoproto.NetworkAccess{
		NetworkName: "private",
		Mode:        yagoproto.NetworkAuthenticationSaltedMagic,
		Essentials:  "shared-secret",
	}
	server := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
			if err := request.ParseForm(); err != nil {
				t.Fatal(err)
			}
			verifier := access
			verifier.Self = self.Hash
			if !verifier.Authorizes(request.PostForm) ||
				request.PostForm.Get(yagoproto.FieldIam) != self.Hash.String() {
				http.Error(w, "unauthorized", http.StatusUnauthorized)

				return
			}
			_, _ = strings.NewReader(yagoproto.QueryResponse{Response: 7}.Encode().Encode()).
				WriteTo(w)
		}),
	)
	defer server.Close()

	count, err := NewRemoteRWICountProbe(
		server.Client(),
		"private",
		self,
		false,
		access,
	).RWICount(t.Context(), serverSeed(t, server))
	if err != nil || count != 7 {
		t.Fatalf("controlled capacity probe = %d, %v", count, err)
	}
}

func TestRemoteRWICountProbeSurfacesSigningFailure(t *testing.T) {
	sentinel := errors.New("signing failed")
	probe := NewRemoteRWICountProbe(
		http.DefaultClient,
		"private",
		yagomodel.Seed{Hash: hashOf(t, "self")},
		false,
		yagoproto.NetworkAccess{Mode: yagoproto.NetworkAuthenticationSaltedMagic},
	)
	probe.signForm = func(url.Values) error { return sentinel }

	_, err := probe.RWICount(t.Context(), yagomodel.Seed{})
	if !errors.Is(err, sentinel) || !errors.Is(err, errCapacityProbeFailed) {
		t.Fatalf("capacity signing error = %v", err)
	}
}

func TestRemoteRWICountProbeRejectsUnreachablePeer(t *testing.T) {
	t.Parallel()

	probe := NewRemoteRWICountProbe(
		http.DefaultClient,
		yagoproto.DefaultNetwork,
		yagomodel.Seed{},
		false,
	)
	if _, err := probe.RWICount(
		context.Background(),
		yagomodel.Seed{Hash: hashOf(t, "peer")},
	); err == nil {
		t.Fatal("expected unreachable peer error")
	}
}

func TestRemoteRWICountProbeRejectsRemoteRejection(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		msg := yagoproto.QueryResponse{Response: yagoproto.QueryResponseRejected}.Encode()
		_, _ = strings.NewReader(msg.Encode()).WriteTo(w)
	}))
	defer server.Close()

	peer := serverSeed(t, server)
	_, err := NewRemoteRWICountProbe(
		server.Client(),
		yagoproto.DefaultNetwork,
		yagomodel.Seed{},
		false,
	).
		RWICount(context.Background(), peer)
	if !errors.Is(err, ErrCapacityProbeRejected) {
		t.Fatalf("error = %v, want ErrCapacityProbeRejected", err)
	}
}

func TestRemoteRWICountProbeRejectsNegativeResponse(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		msg := yagomodel.Message{yagoproto.FieldResponse: "-2"}
		_, _ = strings.NewReader(msg.Encode()).WriteTo(w)
	}))
	defer server.Close()

	peer := serverSeed(t, server)
	if _, err := NewRemoteRWICountProbe(
		server.Client(),
		yagoproto.DefaultNetwork,
		yagomodel.Seed{},
		false,
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
		yagoproto.DefaultNetwork,
		yagomodel.Seed{},
		false,
	)
	peer := peerSeed(t)
	if _, err := probe.RWICount(context.Background(), peer); err == nil {
		t.Fatal("expected protocol error")
	}
}
