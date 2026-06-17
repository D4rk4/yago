package api

import (
	"net/http"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/internal/core"
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
	h.rwi.receipt = core.RWIReceipt{Pause: 5, UnknownURL: []yacymodel.Hash{testHash(t, "url")}}

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
	if resp.Result != yacyproto.TransferRWIResult(yacyproto.ResultOK) {
		t.Errorf("Result = %q, want ok", resp.Result)
	}
	if resp.Pause != 5 {
		t.Errorf("Pause = %d, want 5", resp.Pause)
	}
}

func TestTransferRWIHandlerBusy(t *testing.T) {
	h := newTestHarness(t)
	h.rwi.receipt = core.RWIReceipt{Busy: true}

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
