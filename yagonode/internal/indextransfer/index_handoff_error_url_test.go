package indextransfer

import (
	"reflect"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagoproto"
)

func TestHandoffKeepsRWIErrorURLPostingsUnconfirmed(t *testing.T) {
	t.Parallel()

	rejected := hashOf(t, "url-a")
	accepted := hashOf(t, "url-b")
	postings := []yagomodel.RWIPosting{
		postingOf(t, "word-a", "url-a"),
		postingOf(t, "word-a", "url-b"),
	}
	writer := &recordingWriter{
		rwiResponse: yagoproto.TransferRWIResponse{
			Result:                 yagoproto.ResultOK,
			UnknownURL:             []yagomodel.Hash{rejected, accepted},
			ErrorURL:               []yagomodel.Hash{rejected},
			UnknownURLFieldPresent: true,
		},
		urlResponse: yagoproto.TransferURLResponse{Result: yagoproto.ResultOK},
	}
	urls := &recordingURLDirectory{rows: []yagomodel.URIMetadataRow{rowOf(t, "url-b")}}

	receipt, err := NewHandoff(writer, urls).Send(t.Context(), peerSeed(t), postings)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if receipt.State != HandoffURLSent ||
		!reflect.DeepEqual(urls.gotHashes, []yagomodel.Hash{accepted}) ||
		!reflect.DeepEqual(receipt.RejectedPostings, postings[:1]) {
		t.Fatalf("receipt/hashes = %#v/%#v", receipt, urls.gotHashes)
	}
}

func TestHandoffKeepsTransferURLErrorURLPostingsUnconfirmed(t *testing.T) {
	t.Parallel()

	rejected := hashOf(t, "url-b")
	postings := []yagomodel.RWIPosting{
		postingOf(t, "word-a", "url-a"),
		postingOf(t, "word-a", "url-b"),
	}
	writer := &recordingWriter{
		rwiResponse: yagoproto.TransferRWIResponse{
			Result:                 yagoproto.ResultOK,
			UnknownURL:             []yagomodel.Hash{hashOf(t, "url-a"), rejected},
			UnknownURLFieldPresent: true,
		},
		urlResponse: yagoproto.TransferURLResponse{
			Result:   yagoproto.ResultOK,
			ErrorURL: []yagomodel.Hash{rejected},
		},
	}
	urls := &recordingURLDirectory{rows: []yagomodel.URIMetadataRow{
		rowOf(t, "url-a"),
		rowOf(t, "url-b"),
	}}

	receipt, err := NewHandoff(writer, urls).Send(t.Context(), peerSeed(t), postings)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if receipt.State != HandoffURLSent ||
		!reflect.DeepEqual(receipt.RejectedPostings, postings[1:]) {
		t.Fatalf("receipt = %#v", receipt)
	}
}

func TestHandoffRejectsPostingWhenRequestedMetadataIsUnavailable(t *testing.T) {
	t.Parallel()

	postings := []yagomodel.RWIPosting{postingOf(t, "word-a", "url-a")}
	writer := &recordingWriter{
		rwiResponse: yagoproto.TransferRWIResponse{
			Result:                 yagoproto.ResultOK,
			UnknownURL:             []yagomodel.Hash{hashOf(t, "url-a")},
			UnknownURLFieldPresent: true,
		},
		urlResponse: yagoproto.TransferURLResponse{Result: yagoproto.ResultOK},
	}

	receipt, err := NewHandoff(writer, &recordingURLDirectory{
		rows: []yagomodel.URIMetadataRow{{}},
	}).Send(t.Context(), peerSeed(t), postings)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if receipt.State != HandoffURLSent ||
		!reflect.DeepEqual(receipt.RejectedPostings, postings) {
		t.Fatalf("receipt = %#v", receipt)
	}
}

func TestHandoffIgnoresUnrelatedErrorURLReports(t *testing.T) {
	t.Parallel()

	postings := []yagomodel.RWIPosting{
		postingOf(t, "word-a", "url-a"),
		{WordHash: hashOf(t, "word-b")},
	}
	writer := &recordingWriter{rwiResponse: yagoproto.TransferRWIResponse{
		Result:                 yagoproto.ResultOK,
		ErrorURL:               []yagomodel.Hash{hashOf(t, "url-z")},
		UnknownURLFieldPresent: true,
	}}

	receipt, err := NewHandoff(writer, &recordingURLDirectory{}).Send(
		t.Context(),
		peerSeed(t),
		postings,
	)
	if err != nil || receipt.State != HandoffRWIOnly || len(receipt.RejectedPostings) != 0 {
		t.Fatalf("receipt = %#v, error = %v", receipt, err)
	}
}
