package remotecrawl

import (
	"context"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagoproto"
)

type receiptMetadata struct {
	row  yagomodel.URIMetadataRow
	url  string
	hash yagomodel.URLHash
}

func (b *Broker) acceptReceiptMetadata(
	ctx context.Context,
	request yagoproto.CrawlReceiptRequest,
) (receiptMetadata, bool) {
	if !b.Trusted(request.Iam) {
		b.observe(ctx, Observation{
			Action: "receipt", Outcome: "untrusted", Peer: request.Iam,
		}, true)

		return receiptMetadata{}, false
	}
	if !yagoproto.ValidCrawlReceiptResult(request.Result) {
		b.observe(ctx, Observation{
			Action: "receipt", Outcome: "result_rejected", Peer: request.Iam,
		}, true)

		return receiptMetadata{}, false
	}
	row, decodedURL, hash, err := parseReceiptMetadata(ctx, request.LURLEntry)
	if err != nil {
		b.observe(ctx, Observation{
			Action: "receipt", Outcome: "metadata_rejected", Peer: request.Iam,
		}, true)

		return receiptMetadata{}, false
	}

	return receiptMetadata{row: row, url: decodedURL, hash: hash}, true
}
