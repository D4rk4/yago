package urlmeta

import (
	"context"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
)

func (m urlPorts) endpoint() transferURLEndpoint {
	return transferURLEndpoint{identity: localIdentity(), intake: m.Receiver}
}

func TestTransferURLStoresAndAnswers(t *testing.T) {
	module := openModule(t, 0)

	req := yacyproto.TransferURLRequest{
		NetworkName: "freeworld",
		YouAre:      localIdentity().Hash,
		URLCount:    1,
		URLs:        []yacymodel.URIMetadataRow{urlRow(t, "a")},
	}

	resp, err := module.endpoint().Serve(context.Background(), req)
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if resp.Result != yacyproto.TransferURLResult(yacyproto.ResultOK) {
		t.Fatalf("Result = %q, want ok", resp.Result)
	}
}

func TestTransferURLRejectsWrongNetwork(t *testing.T) {
	module := openModule(t, 0)

	req := yacyproto.TransferURLRequest{NetworkName: "othernet", YouAre: localIdentity().Hash}

	resp, err := module.endpoint().Serve(context.Background(), req)
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if resp.Result != yacyproto.TransferURLResult(yacyproto.ResultWrongTarget) {
		t.Fatalf("Result = %q, want wrong_target", resp.Result)
	}
}
