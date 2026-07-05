package rwi

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagoproto"
)

func (h harness) endpoint() transferRWIEndpoint {
	return transferRWIEndpoint{
		identity: localIdentity(),
		intake:   h.rwi.Receiver,
		batchCap: h.batchCap,
		pause:    5,
	}
}

func TestTransferRWIReportsBusy(t *testing.T) {
	h := openHarness(t, 1, 100)

	req := yagoproto.TransferRWIRequest{
		NetworkName: "freeworld",
		YouAre:      localIdentity().Hash,
		WordCount:   1,
		EntryCount:  1,
		Indexes:     []yagomodel.RWIPosting{posting("w1", "u1")},
	}

	if _, err := h.endpoint().Serve(context.Background(), req); err != nil {
		t.Fatalf("Serve: %v", err)
	}

	req.Indexes = []yagomodel.RWIPosting{posting("w2", "u2")}
	resp, err := h.endpoint().Serve(context.Background(), req)
	if err != nil {
		t.Fatalf("Serve over capacity: %v", err)
	}
	if resp.Result != yagoproto.ResultBusy {
		t.Fatalf("Result = %q, want busy", resp.Result)
	}
	if resp.Pause != 5 {
		t.Fatalf("Pause = %d, want 5", resp.Pause)
	}
}

func TestTransferRWIRejectsOversizedBatch(t *testing.T) {
	h := openHarness(t, 0, 1)

	req := yagoproto.TransferRWIRequest{
		NetworkName: "freeworld",
		YouAre:      localIdentity().Hash,
		WordCount:   2,
		EntryCount:  2,
		Indexes: []yagomodel.RWIPosting{
			posting("w1", "u1"),
			posting("w2", "u2"),
		},
	}

	resp, err := h.endpoint().Serve(context.Background(), req)
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if resp.Result != yagoproto.ResultBusy {
		t.Fatalf("Result = %q, want busy over transfer cap", resp.Result)
	}
	if resp.Pause != 5 {
		t.Fatalf("Pause = %d, want configured backoff", resp.Pause)
	}

	rwiCount, err := h.rwi.Index.RWICount(context.Background())
	if err != nil {
		t.Fatalf("RWICount: %v", err)
	}
	if rwiCount != 0 {
		t.Fatalf("RWICount = %d, want nothing stored for a rejected transfer", rwiCount)
	}
}

func TestTransferRWIStoresAndAnswers(t *testing.T) {
	ctx := context.Background()
	h := openHarness(t, 0, 100)

	req := yagoproto.TransferRWIRequest{
		NetworkName: "freeworld",
		YouAre:      localIdentity().Hash,
		WordCount:   1,
		EntryCount:  1,
		Indexes:     []yagomodel.RWIPosting{posting("w1", "u1")},
	}

	resp, err := h.endpoint().Serve(ctx, req)
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if resp.Result != yagoproto.TransferRWIResult(yagoproto.ResultOK) {
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

	req := yagoproto.TransferRWIRequest{NetworkName: "othernet", YouAre: localIdentity().Hash}

	resp, err := h.endpoint().Serve(context.Background(), req)
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if resp.Result != yagoproto.ResultNotAuthentified {
		t.Fatalf("Result = %q, want not authentified", resp.Result)
	}
	if resp.Pause != transferRWIDefaultPause {
		t.Fatalf("Pause = %d, want default", resp.Pause)
	}
}

func TestTransferRWIRejectsWrongTargetAfterRequiredFields(t *testing.T) {
	h := openHarness(t, 0, 100)

	req := yagoproto.TransferRWIRequest{
		NetworkName: "freeworld",
		YouAre:      yagomodel.Hash("BBBBBBBBBBBB"),
		WordCount:   1,
		EntryCount:  1,
		Indexes:     []yagomodel.RWIPosting{posting("w1", "u1")},
	}

	resp, err := h.endpoint().Serve(context.Background(), req)
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if resp.Result != yagoproto.ResultWrongTarget {
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
		req  yagoproto.TransferRWIRequest
		want yagoproto.TransferRWIResult
	}{
		{
			name: "wordc",
			req: yagoproto.TransferRWIRequest{
				NetworkName: "freeworld",
				YouAre:      localIdentity().Hash,
				EntryCount:  1,
				Indexes:     []yagomodel.RWIPosting{posting("w1", "u1")},
			},
			want: yagoproto.ResultMissingWordC,
		},
		{
			name: "entryc",
			req: yagoproto.TransferRWIRequest{
				NetworkName: "freeworld",
				YouAre:      localIdentity().Hash,
				WordCount:   1,
				Indexes:     []yagomodel.RWIPosting{posting("w1", "u1")},
			},
			want: yagoproto.ResultMissingEntryC,
		},
		{
			name: "indexes",
			req: yagoproto.TransferRWIRequest{
				NetworkName: "freeworld",
				YouAre:      localIdentity().Hash,
				WordCount:   1,
				EntryCount:  1,
			},
			want: yagoproto.ResultMissingIndexes,
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
