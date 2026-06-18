package api

import (
	"bytes"
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/internal/core/contracts"
	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
)

func sampleEntry(tb testing.TB) yacymodel.RWIEntry {
	tb.Helper()

	entry := yacymodel.RWIEntry{
		WordHash: testHash(tb, "word"),
		Properties: map[string]string{
			yacymodel.ColURLHash:        testHash(tb, "url").String(),
			yacymodel.ColLocalLinkCount: "AB",
		},
	}

	roundTrip, err := yacymodel.ParseRWIEntry(entry.String())
	if err != nil {
		tb.Fatalf("entry does not round-trip: %v", err)
	}

	return roundTrip
}

func TestTransferRWIHandlerHappyPath(t *testing.T) {
	h := newTestHarness(t)
	h.rwi.receipt = contracts.RWIReceipt{Pause: 5, UnknownURL: []yacymodel.Hash{testHash(t, "url")}}

	req := yacyproto.TransferRWIRequest{
		YouAre:  h.ident.hash,
		Indexes: []yacymodel.RWIEntry{sampleEntry(t)},
	}
	rec := h.do(t, http.MethodPost, yacyproto.PathTransferRWI, req.Form())

	resp, err := yacyproto.ParseTransferRWIResponse(decodeResponse(t, rec))
	if err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if !h.rwi.called {
		t.Fatal("receiver not called")
	}
	if len(h.rwi.entries) != 1 {
		t.Fatalf("receiver entries = %d, want 1", len(h.rwi.entries))
	}
	if resp.Result != yacyproto.TransferRWIResult(yacyproto.ResultOK) {
		t.Errorf("Result = %q, want ok", resp.Result)
	}
	if resp.Pause != 5 {
		t.Errorf("Pause = %d, want 5", resp.Pause)
	}
}

func TestTransferRWIHandlerAcceptsGzipBody(t *testing.T) {
	h := newTestHarness(t)
	req := yacyproto.TransferRWIRequest{
		YouAre:  h.ident.hash,
		Indexes: []yacymodel.RWIEntry{sampleEntry(t)},
	}

	var body bytes.Buffer
	zw := gzip.NewWriter(&body)
	if _, err := zw.Write([]byte(req.Form().Encode())); err != nil {
		t.Fatalf("write gzip body: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close gzip body: %v", err)
	}

	r := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		yacyproto.PathTransferRWI,
		&body,
	)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Content-Encoding", "gzip")
	rec := httptest.NewRecorder()
	h.mux().ServeHTTP(rec, r)

	resp, err := yacyproto.ParseTransferRWIResponse(decodeResponse(t, rec))
	if err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if resp.Result != yacyproto.TransferRWIResult(yacyproto.ResultOK) {
		t.Errorf("Result = %q, want ok", resp.Result)
	}
	if !h.rwi.called {
		t.Fatal("receiver not called")
	}
}

func TestTransferRWIHandlerBusy(t *testing.T) {
	h := newTestHarness(t)
	h.rwi.receipt = contracts.RWIReceipt{Busy: true}

	req := yacyproto.TransferRWIRequest{
		YouAre:  h.ident.hash,
		Indexes: []yacymodel.RWIEntry{sampleEntry(t)},
	}
	rec := h.do(t, http.MethodPost, yacyproto.PathTransferRWI, req.Form())

	resp, _ := yacyproto.ParseTransferRWIResponse(decodeResponse(t, rec))
	if resp.Result != yacyproto.ResultBusy {
		t.Errorf("Result = %q, want busy", resp.Result)
	}
}

func TestTransferRWIHandlerYouAreMismatch(t *testing.T) {
	h := newTestHarness(t)
	req := yacyproto.TransferRWIRequest{YouAre: testHash(t, "other")}
	rec := h.do(t, http.MethodPost, yacyproto.PathTransferRWI, req.Form())

	resp, _ := yacyproto.ParseTransferRWIResponse(decodeResponse(t, rec))
	if resp.Result != yacyproto.TransferRWIResult(yacyproto.ResultWrongTarget) {
		t.Errorf("Result = %q, want wrong_target", resp.Result)
	}
	if h.rwi.called {
		t.Fatal("receiver must not be called on youare mismatch")
	}
}

func TestTransferRWIHandlerWrongMethod(t *testing.T) {
	h := newTestHarness(t)
	rec := h.do(t, http.MethodGet, yacyproto.PathTransferRWI, nil)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func TestTransferRWIHandlerOversizedBody(t *testing.T) {
	h := newTestHarness(t)
	req := yacyproto.TransferRWIRequest{
		YouAre:  h.ident.hash,
		Indexes: []yacymodel.RWIEntry{sampleEntry(t)},
	}
	rec := h.do(t, http.MethodPost, yacyproto.PathTransferRWI, req.Form(), WithMaxBodyBytes(8))

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rec.Code)
	}
}
