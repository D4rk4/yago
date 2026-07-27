package remotecrawl

import (
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagoproto"
)

func mismatchedHashReceipt(t *testing.T, peer yagomodel.Hash) yagoproto.CrawlReceiptRequest {
	t.Helper()
	hash, err := yagomodel.HashURL(testURLA)
	if err != nil {
		t.Fatal(err)
	}
	row := yagomodel.URIMetadataRow{Properties: map[string]string{
		yagomodel.URLMetaHash: hash.String(),
		yagomodel.URLMetaURL:  yagomodel.EncodeBase64WireForm(testURLB),
	}}

	return yagoproto.CrawlReceiptRequest{
		Iam: peer, Result: yagoproto.CrawlReceiptResultFill,
		LURLEntry: yagomodel.EncodeBase64WireForm(row.String()),
	}
}

func TestRemoteCrawlReceiptRefusalsRemainDistinguishable(t *testing.T) {
	tests := []struct {
		name    string
		request yagoproto.CrawlReceiptRequest
		outcome string
	}{
		{
			name:    "untrusted peer",
			request: metadataReceipt(t, testPeerB, yagoproto.CrawlReceiptResultFill),
			outcome: "untrusted",
		},
		{
			name:    "unsupported result",
			request: metadataReceipt(t, testPeerA, "unsupported"),
			outcome: "result_rejected",
		},
		{
			name:    "metadata hash mismatch",
			request: mismatchedHashReceipt(t, testPeerA),
			outcome: "metadata_rejected",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := &observationRecorder{}
			broker, _ := openMemoryBroker(t, remoteConfig(time.Now), &recordingReceiver{})
			broker.observers = []Observer{recorder}
			response, err := broker.ProcessReceipt(t.Context(), test.request)
			if err != nil || response.Delay != ReceiptRetryDelay {
				t.Fatalf("refused receipt = %+v, %v", response, err)
			}
			if len(recorder.observations) != 1 ||
				recorder.observations[0].Outcome != test.outcome {
				t.Fatalf(
					"receipt observations = %+v, want outcome %q",
					recorder.observations,
					test.outcome,
				)
			}
		})
	}
}
