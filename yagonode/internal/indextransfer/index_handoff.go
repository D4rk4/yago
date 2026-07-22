package indextransfer

import (
	"context"
	"errors"
	"fmt"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagoproto"
)

type PeerWriter interface {
	TransferRWI(
		ctx context.Context,
		peer yagomodel.Seed,
		postings []yagomodel.RWIPosting,
	) (yagoproto.TransferRWIResponse, error)
	TransferURL(
		ctx context.Context,
		peer yagomodel.Seed,
		rows []yagomodel.URIMetadataRow,
	) (yagoproto.TransferURLResponse, error)
}

type URLDirectory interface {
	RowsByHash(ctx context.Context, hashes []yagomodel.Hash) ([]yagomodel.URIMetadataRow, error)
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
	RWI              yagoproto.TransferRWIResponse
	URL              yagoproto.TransferURLResponse
	RemoteUnknownURL []yagomodel.Hash
	RejectedPostings []yagomodel.RWIPosting
	SentURLRows      int
}

func NewHandoff(writer PeerWriter, urls URLDirectory) Handoff {
	return Handoff{writer: writer, urls: urls}
}

func (h Handoff) Send(
	ctx context.Context,
	peer yagomodel.Seed,
	postings []yagomodel.RWIPosting,
) (HandoffReceipt, error) {
	rwi, err := h.writer.TransferRWI(ctx, peer, postings)
	receipt := HandoffReceipt{
		RWI:              rwi,
		RemoteUnknownURL: append([]yagomodel.Hash(nil), rwi.UnknownURL...),
	}
	if err != nil {
		if errors.Is(err, yagoproto.ErrBadField) {
			receipt.State = HandoffRWIRejected
		}
		return receipt, fmt.Errorf("transfer rwi: %w", err)
	}
	if rwi.Result == yagoproto.TransferRWIResult(yagoproto.ResultOK) &&
		!rwi.UnknownURLFieldPresent {
		receipt.State = HandoffRWIRejected
		return receipt, fmt.Errorf(
			"transfer rwi: %w: response missing %s",
			yagoproto.ErrBadField,
			yagoproto.FieldUnknownURL,
		)
	}
	if rwi.Result != yagoproto.TransferRWIResult(yagoproto.ResultOK) {
		receipt.State = HandoffRWIRejected

		return receipt, nil
	}
	rejected := newRejectedURLs(rwi.ErrorURL)
	unknown := rejected.without(rwi.UnknownURL)
	if len(unknown) == 0 {
		receipt.State = HandoffRWIOnly
		receipt.RejectedPostings = rejected.postings(postings)

		return receipt, nil
	}

	rows, err := h.urls.RowsByHash(ctx, unknown)
	if err != nil {
		return receipt, fmt.Errorf("lookup url metadata: %w", err)
	}
	rejected.add(missingMetadataURLs(unknown, rows))
	urls, err := h.writer.TransferURL(ctx, peer, rows)
	receipt.URL = urls
	receipt.SentURLRows = len(rows)
	if err != nil {
		if errors.Is(err, yagoproto.ErrBadField) {
			receipt.State = HandoffURLRejected
		}
		return receipt, fmt.Errorf("transfer url: %w", err)
	}
	if urls.Result != yagoproto.TransferURLResult(yagoproto.ResultOK) {
		receipt.State = HandoffURLRejected

		return receipt, nil
	}

	rejected.add(urls.ErrorURL)
	receipt.State = HandoffURLSent
	receipt.RejectedPostings = rejected.postings(postings)

	return receipt, nil
}
