package api

import (
	"net/http"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/internal/core"
	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
)

func sampleURLRow(tb testing.TB) yacymodel.URIMetadataRow {
	tb.Helper()

	row := yacymodel.URIMetadataRow{
		Properties: map[string]string{yacymodel.URLMetaHash: testHash(tb, "url").String()},
	}

	roundTrip, err := yacymodel.ParseURIMetadataRow(row.String())
	if err != nil {
		tb.Fatalf("row does not round-trip: %v", err)
	}

	return roundTrip
}

func TestTransferURLHandlerHappyPath(t *testing.T) {
	h := newTestHarness(t)
	h.urls.receipt = core.URLReceipt{Double: 2}

	req := yacyproto.TransferURLRequest{
		YouAre:   h.ident.hash,
		URLCount: 1,
		URLs:     []yacymodel.URIMetadataRow{sampleURLRow(t)},
	}
	rec := h.do(t, http.MethodPost, yacyproto.PathTransferURL, req.Form())

	resp, err := yacyproto.ParseTransferURLResponse(decodeResponse(t, rec))
	if err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if !h.urls.called {
		t.Fatal("receiver not called")
	}
	if resp.Result != yacyproto.TransferURLResult(yacyproto.ResultOK) {
		t.Errorf("Result = %q, want ok", resp.Result)
	}
	if resp.Double != 2 {
		t.Errorf("Double = %d, want 2", resp.Double)
	}
}

func TestTransferURLHandlerBusy(t *testing.T) {
	h := newTestHarness(t)
	h.urls.receipt = core.URLReceipt{Busy: true}

	req := yacyproto.TransferURLRequest{
		YouAre:   h.ident.hash,
		URLCount: 1,
		URLs:     []yacymodel.URIMetadataRow{sampleURLRow(t)},
	}
	rec := h.do(t, http.MethodPost, yacyproto.PathTransferURL, req.Form())

	resp, _ := yacyproto.ParseTransferURLResponse(decodeResponse(t, rec))
	if resp.Result != yacyproto.ResultErrorNotGranted {
		t.Errorf("Result = %q, want error_not_granted", resp.Result)
	}
}

func TestTransferURLHandlerWrongNetwork(t *testing.T) {
	h := newTestHarness(t)
	req := yacyproto.TransferURLRequest{NetworkName: "othernet", YouAre: h.ident.hash}
	rec := h.do(t, http.MethodPost, yacyproto.PathTransferURL, req.Form())

	resp, _ := yacyproto.ParseTransferURLResponse(decodeResponse(t, rec))
	if resp.Result != yacyproto.TransferURLResult(yacyproto.ResultWrongTarget) {
		t.Errorf("Result = %q, want wrong_target", resp.Result)
	}
	if h.urls.called {
		t.Fatal("receiver must not be called on network mismatch")
	}
}
