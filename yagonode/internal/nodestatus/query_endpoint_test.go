package nodestatus

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/httpguard"
	"github.com/D4rk4/yago/yagonode/internal/nodeidentity"
	"github.com/D4rk4/yago/yagoproto"
)

type mountedQueryStatus struct{}

func (mountedQueryStatus) Version(context.Context) string {
	return "test-version"
}

func (mountedQueryStatus) Uptime(context.Context) int {
	return 12
}

func newQueryEndpoint(counts stubCounter) queryEndpoint {
	return queryEndpoint{
		identity: nodeidentity.Identity{
			Hash:        yagomodel.WordHash("self"),
			NetworkName: "freeworld",
		},
		rwi:  counts,
		urls: counts,
	}
}

func serveQuery(
	t *testing.T,
	e queryEndpoint,
	req yagoproto.QueryRequest,
) yagoproto.QueryResponse {
	t.Helper()

	resp, err := e.Serve(context.Background(), req)
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}

	return resp
}

func queryRequest(object yagoproto.QueryObject) yagoproto.QueryRequest {
	return yagoproto.QueryRequest{
		NetworkName: "freeworld",
		YouAre:      yagomodel.WordHash("self"),
		Iam:         yagomodel.WordHash("caller"),
		Object:      object,
	}
}

func TestQueryAnswersSupportedObjects(t *testing.T) {
	endpoint := newQueryEndpoint(stubCounter{rwi: 11, rwiURLs: 4, urls: 6})
	word := yagomodel.WordHash("term")

	cases := []struct {
		object yagoproto.QueryObject
		env    string
		want   int
	}{
		{object: yagoproto.ObjectRWICount, want: 11},
		{object: yagoproto.ObjectRWIURLCount, env: word.String(), want: 4},
		{object: yagoproto.ObjectLURLCount, want: 6},
	}
	for _, c := range cases {
		req := queryRequest(c.object)
		req.Env = c.env
		resp := serveQuery(t, endpoint, req)
		if resp.Response != c.want {
			t.Fatalf("%s: Response = %d, want %d", c.object, resp.Response, c.want)
		}
	}
}

func TestQueryAnswersWantedObjectsWithZero(t *testing.T) {
	endpoint := newQueryEndpoint(stubCounter{rwi: 11})

	for _, object := range []yagoproto.QueryObject{
		yagoproto.ObjectWantedLURLs,
		yagoproto.ObjectWantedPURLs,
		yagoproto.ObjectWantedWord,
		yagoproto.ObjectWantedRWI,
		yagoproto.ObjectWantedSeeds,
	} {
		resp := serveQuery(t, endpoint, queryRequest(object))
		if resp.Response != 0 {
			t.Fatalf("%s: Response = %d, want 0", object, resp.Response)
		}
	}
}

func TestQueryAnswersMalformedRWIURLCountEnvWithZero(t *testing.T) {
	endpoint := newQueryEndpoint(stubCounter{rwiURLs: 4})

	resp := serveQuery(t, endpoint, queryRequest(yagoproto.ObjectRWIURLCount))
	if resp.Response != 0 {
		t.Fatalf("Response = %d, want 0", resp.Response)
	}
}

func TestQueryRejectsUnknownObject(t *testing.T) {
	endpoint := newQueryEndpoint(stubCounter{rwi: 11})

	resp := serveQuery(t, endpoint, queryRequest(yagoproto.QueryObject("unknown")))
	if resp.Response != yagoproto.QueryResponseRejected {
		t.Fatalf("Response = %d, want rejected", resp.Response)
	}
}

func TestQueryRejectsWrongTarget(t *testing.T) {
	endpoint := newQueryEndpoint(stubCounter{rwi: 11})

	req := queryRequest(yagoproto.ObjectRWICount)
	req.YouAre = yagomodel.WordHash("other")
	resp := serveQuery(t, endpoint, req)

	if resp.Response != yagoproto.QueryResponseRejected {
		t.Fatalf("Response = %d, want rejected for wrong target", resp.Response)
	}
}

func TestQueryFailsOnCountError(t *testing.T) {
	endpoint := newQueryEndpoint(stubCounter{err: errors.New("boom")})

	if _, err := endpoint.Serve(
		context.Background(),
		queryRequest(yagoproto.ObjectRWICount),
	); err == nil {
		t.Fatal("Serve returned nil error, want count failure")
	}
}

func TestMountQueryServesWireRoute(t *testing.T) {
	mux := http.NewServeMux()
	router := httpguard.NewWireRouter(mux, httpguard.WireGate{
		Guard:   httpguard.NewRequestGuard(1024, time.Second),
		Respond: httpguard.NewWireResponder(mountedQueryStatus{}),
		Address: httpguard.NewClientAddressResolver(nil),
	})
	counts := stubCounter{rwi: 11}
	identity := nodeidentity.Identity{
		Hash:        yagomodel.WordHash("self"),
		NetworkName: "freeworld",
	}
	MountQuery(router, identity, counts, counts)
	form := queryRequest(yagoproto.ObjectRWICount).Form()
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		yagoproto.PathQuery+"?"+form.Encode(),
		nil,
	)

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%q", rec.Code, rec.Body.String())
	}
	msg, err := yagomodel.ParseMessage(rec.Body.String())
	if err != nil {
		t.Fatalf("parse message: %v", err)
	}
	resp, err := yagoproto.ParseQueryResponse(msg)
	if err != nil {
		t.Fatalf("parse query response: %v", err)
	}
	if resp.Response != 11 {
		t.Fatalf("Response = %d, want 11", resp.Response)
	}
	if resp.Version != "test-version" || resp.Uptime != 12 {
		t.Fatalf("header = version %q uptime %d", resp.Version, resp.Uptime)
	}
}
