package nodestatus

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/D4rk4/yago/yacymodel"
	"github.com/D4rk4/yago/yacynode/internal/httpguard"
	"github.com/D4rk4/yago/yacynode/internal/nodeidentity"
	"github.com/D4rk4/yago/yacyproto"
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
			Hash:        yacymodel.WordHash("self"),
			NetworkName: "freeworld",
		},
		rwi:        counts,
		references: counts,
		urls:       counts,
	}
}

func serveQuery(
	t *testing.T,
	e queryEndpoint,
	req yacyproto.QueryRequest,
) yacyproto.QueryResponse {
	t.Helper()

	resp, err := e.Serve(context.Background(), req)
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}

	return resp
}

func queryRequest(object yacyproto.QueryObject) yacyproto.QueryRequest {
	return yacyproto.QueryRequest{
		NetworkName: "freeworld",
		YouAre:      yacymodel.WordHash("self"),
		Iam:         yacymodel.WordHash("caller"),
		Object:      object,
	}
}

func TestQueryAnswersSupportedObjects(t *testing.T) {
	endpoint := newQueryEndpoint(stubCounter{rwi: 11, refs: 4, urls: 6})

	cases := []struct {
		object yacyproto.QueryObject
		want   int
	}{
		{yacyproto.ObjectRWICount, 11},
		{yacyproto.ObjectRWIURLCount, 4},
		{yacyproto.ObjectLURLCount, 6},
	}
	for _, c := range cases {
		resp := serveQuery(t, endpoint, queryRequest(c.object))
		if resp.Response != c.want {
			t.Fatalf("%s: Response = %d, want %d", c.object, resp.Response, c.want)
		}
	}
}

func TestQueryRejectsUnsupportedObject(t *testing.T) {
	endpoint := newQueryEndpoint(stubCounter{rwi: 11})

	resp := serveQuery(t, endpoint, queryRequest(yacyproto.ObjectWantedSeeds))
	if resp.Response != yacyproto.QueryResponseRejected {
		t.Fatalf("Response = %d, want rejected", resp.Response)
	}
}

func TestQueryRejectsWrongTarget(t *testing.T) {
	endpoint := newQueryEndpoint(stubCounter{rwi: 11})

	req := queryRequest(yacyproto.ObjectRWICount)
	req.YouAre = yacymodel.WordHash("other")
	resp := serveQuery(t, endpoint, req)

	if resp.Response != yacyproto.QueryResponseRejected {
		t.Fatalf("Response = %d, want rejected for wrong target", resp.Response)
	}
}

func TestQueryFailsOnCountError(t *testing.T) {
	endpoint := newQueryEndpoint(stubCounter{err: errors.New("boom")})

	if _, err := endpoint.Serve(
		context.Background(),
		queryRequest(yacyproto.ObjectRWICount),
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
		Hash:        yacymodel.WordHash("self"),
		NetworkName: "freeworld",
	}
	MountQuery(router, identity, counts, counts, counts)
	form := queryRequest(yacyproto.ObjectRWICount).Form()
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		yacyproto.PathQuery+"?"+form.Encode(),
		nil,
	)

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%q", rec.Code, rec.Body.String())
	}
	msg, err := yacymodel.ParseMessage(rec.Body.String())
	if err != nil {
		t.Fatalf("parse message: %v", err)
	}
	resp, err := yacyproto.ParseQueryResponse(msg)
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
