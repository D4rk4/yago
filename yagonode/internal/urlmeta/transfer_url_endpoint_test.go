package urlmeta

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagoproto"
)

func (m urlPorts) endpoint() transferURLEndpoint {
	return transferURLEndpoint{identity: localIdentity(), intake: m.Receiver}
}

func TestTransferURLStoresAndAnswers(t *testing.T) {
	module := openModule(t, 0)

	req := yagoproto.TransferURLRequest{
		NetworkName: "freeworld",
		YouAre:      localIdentity().Hash,
		URLCount:    1,
		URLs:        []yagomodel.URIMetadataRow{urlRow(t, "a")},
	}

	resp, err := module.endpoint().Serve(context.Background(), req)
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if resp.Result != yagoproto.TransferURLResult(yagoproto.ResultOK) {
		t.Fatalf("Result = %q, want ok", resp.Result)
	}
}

func TestTransferURLRejectsWrongNetwork(t *testing.T) {
	module := openModule(t, 0)

	req := yagoproto.TransferURLRequest{NetworkName: "othernet", YouAre: localIdentity().Hash}

	resp, err := module.endpoint().Serve(context.Background(), req)
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if resp.Result != "" {
		t.Fatalf("Result = %q, want empty auth failure response", resp.Result)
	}
	if len(resp.Encode()) != 0 {
		t.Fatalf("encoded response = %+v, want empty transfer fields", resp.Encode())
	}
}

func TestTransferURLRejectsWrongTargetAfterNetworkAuth(t *testing.T) {
	module := openModule(t, 0)

	req := yagoproto.TransferURLRequest{
		NetworkName: "freeworld",
		YouAre:      yagomodel.Hash("BBBBBBBBBBBB"),
	}

	resp, err := module.endpoint().Serve(context.Background(), req)
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if resp.Result != yagoproto.TransferURLResult(yagoproto.ResultWrongTarget) {
		t.Fatalf("Result = %q, want wrong_target", resp.Result)
	}
}
