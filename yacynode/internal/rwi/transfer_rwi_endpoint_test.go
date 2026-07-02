package rwi

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yacymodel"
	"github.com/D4rk4/yago/yacyproto"
)

func (h harness) endpoint() transferRWIEndpoint {
	return transferRWIEndpoint{identity: localIdentity(), intake: h.rwi.Receiver}
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

	if _, err := h.endpoint().Serve(context.Background(), req); err != nil {
		t.Fatalf("Serve: %v", err)
	}

	req.Indexes = []yacymodel.RWIPosting{posting("w2", "u2")}
	resp, err := h.endpoint().Serve(context.Background(), req)
	if err != nil {
		t.Fatalf("Serve over capacity: %v", err)
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

	rwiCount, err := h.rwi.Index.RWICount(ctx)
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
	if resp.Result != yacyproto.ResultNotAuthentified {
		t.Fatalf("Result = %q, want not authentified", resp.Result)
	}
	if resp.Pause != transferRWIDefaultPause {
		t.Fatalf("Pause = %d, want default", resp.Pause)
	}
}

func TestTransferRWIRejectsWrongTargetAfterRequiredFields(t *testing.T) {
	h := openHarness(t, 0, 100)

	req := yacyproto.TransferRWIRequest{
		NetworkName: "freeworld",
		YouAre:      yacymodel.Hash("BBBBBBBBBBBB"),
		WordCount:   1,
		EntryCount:  1,
		Indexes:     []yacymodel.RWIPosting{posting("w1", "u1")},
	}

	resp, err := h.endpoint().Serve(context.Background(), req)
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if resp.Result != yacyproto.ResultWrongTarget {
		t.Fatalf("Result = %q, want wrong_target", resp.Result)
	}
	if resp.Pause != 0 {
		t.Fatalf("Pause = %d, want 0", resp.Pause)
	}
}

func TestTransferRWIReportsMissingRequiredFields(t *testing.T) {
	endpoint := transferRWIEndpoint{identity: localIdentity(), intake: fakePostingReceiver{}}

	for _, item := range []struct {
		name string
		req  yacyproto.TransferRWIRequest
		want yacyproto.TransferRWIResult
	}{
		{
			name: "wordc",
			req: yacyproto.TransferRWIRequest{
				NetworkName: "freeworld",
				YouAre:      localIdentity().Hash,
				EntryCount:  1,
				Indexes:     []yacymodel.RWIPosting{posting("w1", "u1")},
			},
			want: yacyproto.ResultMissingWordC,
		},
		{
			name: "entryc",
			req: yacyproto.TransferRWIRequest{
				NetworkName: "freeworld",
				YouAre:      localIdentity().Hash,
				WordCount:   1,
				Indexes:     []yacymodel.RWIPosting{posting("w1", "u1")},
			},
			want: yacyproto.ResultMissingEntryC,
		},
		{
			name: "indexes",
			req: yacyproto.TransferRWIRequest{
				NetworkName: "freeworld",
				YouAre:      localIdentity().Hash,
				WordCount:   1,
				EntryCount:  1,
			},
			want: yacyproto.ResultMissingIndexes,
		},
	} {
		t.Run(item.name, func(t *testing.T) {
			resp, err := endpoint.Serve(context.Background(), item.req)
			if err != nil {
				t.Fatalf("Serve: %v", err)
			}
			if resp.Result != item.want {
				t.Fatalf("Result = %q, want %q", resp.Result, item.want)
			}
			if resp.Pause != transferRWIDefaultPause {
				t.Fatalf("Pause = %d, want default", resp.Pause)
			}
		})
	}
}
