package rwi

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagoproto"
)

func (h harness) endpoint() transferRWIEndpoint {
	return transferRWIEndpoint{
		identity:          localIdentity(),
		intake:            h.rwi.Receiver,
		batchCap:          h.batchCap,
		pauseMilliseconds: 5000,
		accept:            true,
	}
}

// TestTransferRWIRefusesWhenAcceptRemoteIndexOff: with the accept-remote-index
// capability off, a valid transfer is answered not_granted (YaCy transferRWI
// with allowReceiveIndex disabled) and nothing reaches the intake.
func TestTransferRWIRefusesWhenAcceptRemoteIndexOff(t *testing.T) {
	endpoint := transferRWIEndpoint{identity: localIdentity(), intake: fakePostingReceiver{}}

	resp, err := endpoint.Serve(context.Background(), yagoproto.TransferRWIRequest{
		NetworkName: "freeworld",
		YouAre:      localIdentity().Hash,
		WordCount:   1,
		EntryCount:  1,
		Indexes:     []yagomodel.RWIPosting{posting("w1", "u1")},
	})
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if resp.Result != yagoproto.ResultNotGranted {
		t.Fatalf("Result = %q, want not_granted", resp.Result)
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

	ctx := context.Background()
	if _, err := h.endpoint().Serve(ctx, req); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if _, err := h.vault.UsedBytes(ctx); err != nil {
		t.Fatalf("UsedBytes: %v", err)
	}

	req.Indexes = []yagomodel.RWIPosting{posting("w2", "u2")}
	resp, err := h.endpoint().Serve(ctx, req)
	if err != nil {
		t.Fatalf("Serve over capacity: %v", err)
	}
	if resp.Result != yagoproto.ResultBusy {
		t.Fatalf("Result = %q, want busy", resp.Result)
	}
	if resp.Pause != 5000 {
		t.Fatalf("Pause = %d, want 5000", resp.Pause)
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
	if resp.Pause != 5000 {
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

func TestTransferRWIRejectsOversizedDeclaredBatchWithoutIntake(t *testing.T) {
	h := openHarness(t, 0, yagoproto.MaximumTransferEntries)
	response, err := h.endpoint().Serve(t.Context(), yagoproto.TransferRWIRequest{
		NetworkName: "freeworld",
		YouAre:      localIdentity().Hash,
		WordCount:   1,
		EntryCount:  yagoproto.MaximumTransferEntries + 1,
		Indexes:     []yagomodel.RWIPosting{posting("w1", "u1")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Result != yagoproto.ResultBusy || response.Pause != 5000 {
		t.Fatalf("response = %+v, want busy pause", response)
	}
	count, err := h.rwi.Index.RWICount(t.Context())
	if err != nil || count != 0 {
		t.Fatalf("stored rows = %d, %v", count, err)
	}
}

func TestTransferRWIRejectsOversizedParsedPayloadWithoutIntake(t *testing.T) {
	h := openHarness(t, 0, yagoproto.MaximumTransferEntries)
	entry := posting("w1", "u1").String()
	request, err := yagoproto.ParseTransferRWIRequest(t.Context(), url.Values{
		yagoproto.FieldNetworkName: {"freeworld"},
		yagoproto.FieldYouAre:      {localIdentity().Hash.String()},
		yagoproto.FieldWordCount:   {"1"},
		yagoproto.FieldEntryCount:  {"1000"},
		yagoproto.FieldIndexes: {
			strings.Repeat(entry+"\n", yagoproto.MaximumTransferEntries+1),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	response, err := h.endpoint().Serve(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if response.Result != yagoproto.ResultBusy || response.Pause != 5000 {
		t.Fatalf("response = %+v, want busy pause", response)
	}
	count, err := h.rwi.Index.RWICount(t.Context())
	if err != nil || count != 0 {
		t.Fatalf("stored rows = %d, %v", count, err)
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
	endpoint := transferRWIEndpoint{
		identity: localIdentity(),
		intake:   fakePostingReceiver{},
		accept:   true,
	}

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
