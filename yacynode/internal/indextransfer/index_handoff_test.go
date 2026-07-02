package indextransfer

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/D4rk4/yago/yacymodel"
	"github.com/D4rk4/yago/yacyproto"
)

type recordingWriter struct {
	rwiResponse yacyproto.TransferRWIResponse
	rwiErr      error
	urlResponse yacyproto.TransferURLResponse
	urlErr      error

	rwiCalls    int
	urlCalls    int
	gotPeer     yacymodel.Seed
	gotPostings []yacymodel.RWIPosting
	gotRows     []yacymodel.URIMetadataRow
}

func (w *recordingWriter) TransferRWI(
	_ context.Context,
	peer yacymodel.Seed,
	postings []yacymodel.RWIPosting,
) (yacyproto.TransferRWIResponse, error) {
	w.rwiCalls++
	w.gotPeer = peer
	w.gotPostings = append([]yacymodel.RWIPosting(nil), postings...)

	return w.rwiResponse, w.rwiErr
}

func (w *recordingWriter) TransferURL(
	_ context.Context,
	peer yacymodel.Seed,
	rows []yacymodel.URIMetadataRow,
) (yacyproto.TransferURLResponse, error) {
	w.urlCalls++
	w.gotPeer = peer
	w.gotRows = append([]yacymodel.URIMetadataRow(nil), rows...)

	return w.urlResponse, w.urlErr
}

type recordingURLDirectory struct {
	rows []yacymodel.URIMetadataRow
	err  error

	calls     int
	gotHashes []yacymodel.Hash
}

func (d *recordingURLDirectory) RowsByHash(
	_ context.Context,
	hashes []yacymodel.Hash,
) ([]yacymodel.URIMetadataRow, error) {
	d.calls++
	d.gotHashes = append([]yacymodel.Hash(nil), hashes...)

	return d.rows, d.err
}

func TestHandoffStopsWhenRWITransferHasNoUnknownURLs(t *testing.T) {
	t.Parallel()

	writer := &recordingWriter{
		rwiResponse: yacyproto.TransferRWIResponse{Result: yacyproto.ResultOK},
	}
	urls := &recordingURLDirectory{}
	postings := []yacymodel.RWIPosting{postingOf(t, "word-a", "url-a")}
	peer := peerSeed(t)

	receipt, err := NewHandoff(writer, urls).Send(context.Background(), peer, postings)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	if receipt.State != HandoffRWIOnly {
		t.Fatalf("State = %q, want %q", receipt.State, HandoffRWIOnly)
	}
	if writer.rwiCalls != 1 || writer.urlCalls != 0 || urls.calls != 0 {
		t.Fatalf(
			"calls = rwi %d url %d rows %d",
			writer.rwiCalls,
			writer.urlCalls,
			urls.calls,
		)
	}
	if !reflect.DeepEqual(writer.gotPostings, postings) || writer.gotPeer.Hash != peer.Hash {
		t.Fatalf("rwi input mismatch: peer %#v postings %#v", writer.gotPeer, writer.gotPostings)
	}
}

func TestHandoffStopsWhenPeerRejectsRWI(t *testing.T) {
	t.Parallel()

	writer := &recordingWriter{
		rwiResponse: yacyproto.TransferRWIResponse{Result: yacyproto.ResultBusy, Pause: 9},
	}
	urls := &recordingURLDirectory{}

	receipt, err := NewHandoff(writer, urls).Send(
		context.Background(),
		peerSeed(t),
		[]yacymodel.RWIPosting{postingOf(t, "word-a", "url-a")},
	)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	if receipt.State != HandoffRWIRejected || receipt.RWI.Pause != 9 {
		t.Fatalf("receipt = %#v", receipt)
	}
	if writer.urlCalls != 0 || urls.calls != 0 {
		t.Fatalf("unexpected follow-up calls: url %d rows %d", writer.urlCalls, urls.calls)
	}
}

func TestHandoffSendsRowsForRemoteUnknownURLs(t *testing.T) {
	t.Parallel()

	unknown := []yacymodel.Hash{hashOf(t, "url-a"), hashOf(t, "url-b")}
	rows := []yacymodel.URIMetadataRow{rowOf(t, "url-a"), rowOf(t, "url-b")}
	writer := &recordingWriter{
		rwiResponse: yacyproto.TransferRWIResponse{
			Result:     yacyproto.ResultOK,
			UnknownURL: unknown,
		},
		urlResponse: yacyproto.TransferURLResponse{
			Result: yacyproto.TransferURLResult(yacyproto.ResultOK),
			Double: 1,
		},
	}
	urls := &recordingURLDirectory{rows: rows}

	receipt, err := NewHandoff(writer, urls).Send(
		context.Background(),
		peerSeed(t),
		[]yacymodel.RWIPosting{postingOf(t, "word-a", "url-a")},
	)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	if receipt.State != HandoffURLSent || receipt.SentURLRows != 2 || receipt.URL.Double != 1 {
		t.Fatalf("receipt = %#v", receipt)
	}
	if !reflect.DeepEqual(urls.gotHashes, unknown) {
		t.Fatalf("RowsByHash hashes = %#v, want %#v", urls.gotHashes, unknown)
	}
	if !reflect.DeepEqual(writer.gotRows, rows) {
		t.Fatalf("TransferURL rows = %#v, want %#v", writer.gotRows, rows)
	}
	unknown[0] = hashOf(t, "url-c")
	if receipt.RemoteUnknownURL[0] == unknown[0] {
		t.Fatal("receipt kept caller-owned unknown URL slice")
	}
}

func TestHandoffStillPostsURLTransferWhenRowsAreMissingLocally(t *testing.T) {
	t.Parallel()

	writer := &recordingWriter{
		rwiResponse: yacyproto.TransferRWIResponse{
			Result:     yacyproto.ResultOK,
			UnknownURL: []yacymodel.Hash{hashOf(t, "url-a")},
		},
		urlResponse: yacyproto.TransferURLResponse{
			Result: yacyproto.TransferURLResult(yacyproto.ResultOK),
		},
	}
	urls := &recordingURLDirectory{}

	receipt, err := NewHandoff(writer, urls).Send(
		context.Background(),
		peerSeed(t),
		[]yacymodel.RWIPosting{postingOf(t, "word-a", "url-a")},
	)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	if receipt.State != HandoffURLSent || receipt.SentURLRows != 0 {
		t.Fatalf("receipt = %#v", receipt)
	}
	if writer.urlCalls != 1 || len(writer.gotRows) != 0 {
		t.Fatalf("url transfer = calls %d rows %#v", writer.urlCalls, writer.gotRows)
	}
}

func TestHandoffStopsWhenPeerRejectsURLRows(t *testing.T) {
	t.Parallel()

	rejected := hashOf(t, "url-a")
	writer := &recordingWriter{
		rwiResponse: yacyproto.TransferRWIResponse{
			Result:     yacyproto.ResultOK,
			UnknownURL: []yacymodel.Hash{rejected},
		},
		urlResponse: yacyproto.TransferURLResponse{
			Result:   yacyproto.ResultErrorNotGranted,
			ErrorURL: []yacymodel.Hash{rejected},
		},
	}
	urls := &recordingURLDirectory{rows: []yacymodel.URIMetadataRow{rowOf(t, "url-a")}}

	receipt, err := NewHandoff(writer, urls).Send(
		context.Background(),
		peerSeed(t),
		[]yacymodel.RWIPosting{postingOf(t, "word-a", "url-a")},
	)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	if receipt.State != HandoffURLRejected ||
		!reflect.DeepEqual(receipt.URL.ErrorURL, []yacymodel.Hash{rejected}) {
		t.Fatalf("receipt = %#v", receipt)
	}
}

func TestHandoffReportsTransferAndLookupErrors(t *testing.T) {
	t.Parallel()

	rwiErr := errors.New("rwi failed")
	_, err := NewHandoff(
		&recordingWriter{rwiErr: rwiErr},
		&recordingURLDirectory{},
	).Send(context.Background(), peerSeed(t), []yacymodel.RWIPosting{postingOf(t, "word-a", "url-a")})
	if !errors.Is(err, rwiErr) {
		t.Fatalf("rwi err = %v, want %v", err, rwiErr)
	}

	rowErr := errors.New("rows failed")
	_, err = NewHandoff(
		&recordingWriter{
			rwiResponse: yacyproto.TransferRWIResponse{
				Result:     yacyproto.ResultOK,
				UnknownURL: []yacymodel.Hash{hashOf(t, "url-a")},
			},
		},
		&recordingURLDirectory{err: rowErr},
	).Send(context.Background(), peerSeed(t), []yacymodel.RWIPosting{postingOf(t, "word-a", "url-a")})
	if !errors.Is(err, rowErr) {
		t.Fatalf("row err = %v, want %v", err, rowErr)
	}

	urlErr := errors.New("url failed")
	receipt, err := NewHandoff(
		&recordingWriter{
			rwiResponse: yacyproto.TransferRWIResponse{
				Result:     yacyproto.ResultOK,
				UnknownURL: []yacymodel.Hash{hashOf(t, "url-a")},
			},
			urlErr: urlErr,
		},
		&recordingURLDirectory{rows: []yacymodel.URIMetadataRow{rowOf(t, "url-a")}},
	).Send(context.Background(), peerSeed(t), []yacymodel.RWIPosting{postingOf(t, "word-a", "url-a")})
	if !errors.Is(err, urlErr) || receipt.SentURLRows != 1 {
		t.Fatalf("url err = %v receipt = %#v", err, receipt)
	}
}
