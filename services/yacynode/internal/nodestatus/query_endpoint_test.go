package nodestatus

import (
	"context"
	"errors"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/nodeidentity"
	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
)

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
