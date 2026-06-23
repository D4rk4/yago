package rwi

import (
	"context"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
)

func (h harness) endpoint() transferRWIEndpoint {
	return transferRWIEndpoint{peer: localIdentity(), intake: h.rwi.Receiver}
}

func TestTransferRWIReportsBusy(t *testing.T) {
	h := openHarness(t, 1, 100)

	req := yacyproto.TransferRWIRequest{
		NetworkName: "freeworld",
		YouAre:      localIdentity().Hash,
		WordCount:   1,
		EntryCount:  1,
		Indexes:     []yacymodel.RWIPosting{posting("w1", "u1")},
	}

	resp, err := h.endpoint().Serve(context.Background(), req)
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if resp.Result != yacyproto.ResultBusy {
		t.Fatalf("Result = %q, want busy", resp.Result)
	}
	if resp.Pause != 5 {
		t.Fatalf("Pause = %d, want 5", resp.Pause)
	}
}

func TestTransferRWIStoresAndAnswers(t *testing.T) {
	ctx := context.Background()
	h := openHarness(t, 0, 100)

	req := yacyproto.TransferRWIRequest{
		NetworkName: "freeworld",
		YouAre:      localIdentity().Hash,
		WordCount:   1,
		EntryCount:  1,
		Indexes:     []yacymodel.RWIPosting{posting("w1", "u1")},
	}

	resp, err := h.endpoint().Serve(ctx, req)
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if resp.Result != yacyproto.TransferRWIResult(yacyproto.ResultOK) {
		t.Fatalf("Result = %q, want ok", resp.Result)
	}

	rwiCount, err := h.rwi.Directory.RWICount(ctx)
	if err != nil {
		t.Fatalf("RWICount: %v", err)
	}
	if rwiCount != 1 {
		t.Fatalf("RWICount = %d, want 1", rwiCount)
	}
}

func TestTransferRWIRejectsWrongNetwork(t *testing.T) {
	h := openHarness(t, 0, 100)

	req := yacyproto.TransferRWIRequest{NetworkName: "othernet", YouAre: localIdentity().Hash}

	resp, err := h.endpoint().Serve(context.Background(), req)
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if resp.Result != yacyproto.TransferRWIResult(yacyproto.ResultWrongTarget) {
		t.Fatalf("Result = %q, want wrong_target", resp.Result)
	}
}
