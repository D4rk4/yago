package api

import (
	"net/http"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/internal/core"
	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
)

func TestQueryHandlerSupportedObject(t *testing.T) {
	h := newTestHarness(t)
	h.counter.count = 42

	req := yacyproto.QueryRequest{YouAre: h.ident.hash, Object: yacyproto.ObjectRWICount}
	rec := h.do(t, http.MethodPost, yacyproto.PathQuery, req.Form())

	resp, err := yacyproto.ParseQueryResponse(decodeResponse(t, rec))
	if err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if h.counter.kind != core.RWICount {
		t.Errorf("kind = %v, want RWICount", h.counter.kind)
	}
	if resp.Response != 42 {
		t.Errorf("Response = %d, want 42", resp.Response)
	}
}

func TestQueryHandlerUnsupportedObject(t *testing.T) {
	h := newTestHarness(t)
	req := yacyproto.QueryRequest{YouAre: h.ident.hash, Object: yacyproto.ObjectWantedSeeds}
	rec := h.do(t, http.MethodPost, yacyproto.PathQuery, req.Form())

	resp, _ := yacyproto.ParseQueryResponse(decodeResponse(t, rec))
	if resp.Response != yacyproto.QueryResponseRejected {
		t.Errorf("Response = %d, want -1", resp.Response)
	}
	if h.counter.called {
		t.Fatal("counter must not be called for unsupported object")
	}
}

func TestQueryHandlerYouAreMismatch(t *testing.T) {
	h := newTestHarness(t)
	req := yacyproto.QueryRequest{YouAre: testHash(t, "other"), Object: yacyproto.ObjectRWICount}
	rec := h.do(t, http.MethodPost, yacyproto.PathQuery, req.Form())

	resp, _ := yacyproto.ParseQueryResponse(decodeResponse(t, rec))
	if resp.Response != yacyproto.QueryResponseRejected {
		t.Errorf("Response = %d, want -1", resp.Response)
	}
	if h.counter.called {
		t.Fatal("counter must not be called on youare mismatch")
	}
}
