package indextransfer

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yacymodel"
	"github.com/D4rk4/yago/yacyproto"
)

type PeerWriter interface {
	TransferRWI(
		ctx context.Context,
		peer yacymodel.Seed,
		postings []yacymodel.RWIPosting,
	) (yacyproto.TransferRWIResponse, error)
	TransferURL(
		ctx context.Context,
		peer yacymodel.Seed,
		rows []yacymodel.URIMetadataRow,
	) (yacyproto.TransferURLResponse, error)
}

type URLDirectory interface {
	RowsByHash(ctx context.Context, hashes []yacymodel.Hash) ([]yacymodel.URIMetadataRow, error)
}

type Handoff struct {
	writer PeerWriter
	urls   URLDirectory
}

type HandoffState string

const (
	HandoffRWIOnly     HandoffState = "rwi_only"
	HandoffURLSent     HandoffState = "url_sent"
	HandoffRWIRejected HandoffState = "rwi_rejected"
	HandoffURLRejected HandoffState = "url_rejected"
)

type HandoffReceipt struct {
	State            HandoffState
	RWI              yacyproto.TransferRWIResponse
	URL              yacyproto.TransferURLResponse
	RemoteUnknownURL []yacymodel.Hash
	SentURLRows      int
}

func NewHandoff(writer PeerWriter, urls URLDirectory) Handoff {
	return Handoff{writer: writer, urls: urls}
}

func (h Handoff) Send(
	ctx context.Context,
	peer yacymodel.Seed,
	postings []yacymodel.RWIPosting,
) (HandoffReceipt, error) {
	rwi, err := h.writer.TransferRWI(ctx, peer, postings)
	receipt := HandoffReceipt{
		RWI:              rwi,
		RemoteUnknownURL: append([]yacymodel.Hash(nil), rwi.UnknownURL...),
	}
	if err != nil {
		return receipt, fmt.Errorf("transfer rwi: %w", err)
	}
	if rwi.Result != yacyproto.TransferRWIResult(yacyproto.ResultOK) {
		receipt.State = HandoffRWIRejected

		return receipt, nil
	}
	if len(rwi.UnknownURL) == 0 {
		receipt.State = HandoffRWIOnly

		return receipt, nil
	}

	rows, err := h.urls.RowsByHash(ctx, rwi.UnknownURL)
	if err != nil {
		return receipt, fmt.Errorf("lookup url metadata: %w", err)
	}
	urls, err := h.writer.TransferURL(ctx, peer, rows)
	receipt.URL = urls
	receipt.SentURLRows = len(rows)
	if err != nil {
		return receipt, fmt.Errorf("transfer url: %w", err)
	}
	if urls.Result != yacyproto.TransferURLResult(yacyproto.ResultOK) {
		receipt.State = HandoffURLRejected

		return receipt, nil
	}

	receipt.State = HandoffURLSent

	return receipt, nil
}
