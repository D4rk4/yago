package nodestatus

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/internal/httpguard"
	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
)

func newQueryEndpoint(counts stubCounter) queryEndpoint {
	guard := httpguard.NewRequestGuard(
		httpguard.LocalPeer{Hash: yacymodel.WordHash("self"), NetworkName: "freeworld"},
		httpguard.DefaultMaxBodyBytes,
		time.Second,
	)
	report := newReport(testIdentity(), counts, counts, fixedClock(time.Unix(0, 0).UTC(), 0))

	return queryEndpoint{guard: guard, report: report, rwi: counts, urls: counts}
}

func serveQuery(
	t *testing.T,
	e queryEndpoint,
	req yacyproto.QueryRequest,
) *httptest.ResponseRecorder {
	t.Helper()

	rec := httptest.NewRecorder()
	httpReq := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		yacyproto.PathQuery,
		nil,
	)
	httpReq.PostForm = req.Form()
	e.ServeHTTP(rec, httpReq)

	return rec
}

func queryRequest(object yacyproto.QueryObject) yacyproto.QueryRequest {
	return yacyproto.QueryRequest{
		NetworkName: "freeworld",
		YouAre:      yacymodel.WordHash("self"),
		Iam:         yacymodel.WordHash("caller"),
		Object:      object,
	}
}

func parseResponse(t *testing.T, rec *httptest.ResponseRecorder) yacyproto.QueryResponse {
	t.Helper()

	message, err := yacymodel.ParseMessage(rec.Body.String())
	if err != nil {
		t.Fatalf("ParseMessage: %v", err)
	}
	resp, err := yacyproto.ParseQueryResponse(message)
	if err != nil {
		t.Fatalf("ParseQueryResponse: %v", err)
	}

	return resp
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
		rec := serveQuery(t, endpoint, queryRequest(c.object))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200", c.object, rec.Code)
		}
		resp := parseResponse(t, rec)
		if resp.Response != c.want {
			t.Fatalf("%s: Response = %d, want %d", c.object, resp.Response, c.want)
		}
		if resp.Version != "1.2" {
			t.Fatalf("%s: Version = %q, want 1.2", c.object, resp.Version)
		}
	}
}

func TestQueryRejectsUnsupportedObject(t *testing.T) {
	endpoint := newQueryEndpoint(stubCounter{rwi: 11})

	rec := serveQuery(t, endpoint, queryRequest(yacyproto.ObjectWantedSeeds))

	resp := parseResponse(t, rec)
	if resp.Response != yacyproto.QueryResponseRejected {
		t.Fatalf("Response = %d, want rejected", resp.Response)
	}
}

func TestQueryRejectsWrongTarget(t *testing.T) {
	endpoint := newQueryEndpoint(stubCounter{rwi: 11})

	req := queryRequest(yacyproto.ObjectRWICount)
	req.YouAre = yacymodel.WordHash("other")
	rec := serveQuery(t, endpoint, req)

	resp := parseResponse(t, rec)
	if resp.Response != yacyproto.QueryResponseRejected {
		t.Fatalf("Response = %d, want rejected for wrong target", resp.Response)
	}
}

func TestQueryFailsOnCountError(t *testing.T) {
	endpoint := newQueryEndpoint(stubCounter{err: errors.New("boom")})

	rec := serveQuery(t, endpoint, queryRequest(yacyproto.ObjectRWICount))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}
