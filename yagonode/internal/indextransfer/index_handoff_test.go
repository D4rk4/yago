package indextransfer

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagoproto"
)

type recordingWriter struct {
	rwiResponse yagoproto.TransferRWIResponse
	rwiErr      error
	urlResponse yagoproto.TransferURLResponse
	urlErr      error

	rwiCalls    int
	urlCalls    int
	gotPeer     yagomodel.Seed
	gotPostings []yagomodel.RWIPosting
	gotRows     []yagomodel.URIMetadataRow
}

func (w *recordingWriter) TransferRWI(
	_ context.Context,
	peer yagomodel.Seed,
	postings []yagomodel.RWIPosting,
) (yagoproto.TransferRWIResponse, error) {
	w.rwiCalls++
	w.gotPeer = peer
	w.gotPostings = append([]yagomodel.RWIPosting(nil), postings...)

	return w.rwiResponse, w.rwiErr
}

func (w *recordingWriter) TransferURL(
	_ context.Context,
	peer yagomodel.Seed,
	rows []yagomodel.URIMetadataRow,
) (yagoproto.TransferURLResponse, error) {
	w.urlCalls++
	w.gotPeer = peer
	w.gotRows = append([]yagomodel.URIMetadataRow(nil), rows...)

	return w.urlResponse, w.urlErr
}

type recordingURLDirectory struct {
	rows []yagomodel.URIMetadataRow
	err  error

	calls     int
	gotHashes []yagomodel.Hash
}

func (d *recordingURLDirectory) RowsByHash(
	_ context.Context,
	hashes []yagomodel.Hash,
) ([]yagomodel.URIMetadataRow, error) {
	d.calls++
	d.gotHashes = append([]yagomodel.Hash(nil), hashes...)

	return d.rows, d.err
}

func TestHandoffStopsWhenRWITransferHasPresentEmptyUnknownURLs(t *testing.T) {
	t.Parallel()

	writer := &recordingWriter{
		rwiResponse: yagoproto.TransferRWIResponse{
			Result:                 yagoproto.ResultOK,
			UnknownURLFieldPresent: true,
		},
	}
	urls := &recordingURLDirectory{}
	postings := []yagomodel.RWIPosting{postingOf(t, "word-a", "url-a")}
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

func TestHandoffRejectsOKWithoutUnknownURLField(t *testing.T) {
	t.Parallel()

	writer := &recordingWriter{
		rwiResponse: yagoproto.TransferRWIResponse{Result: yagoproto.ResultOK},
	}
	receipt, err := NewHandoff(writer, &recordingURLDirectory{}).Send(
		context.Background(),
		peerSeed(t),
		[]yagomodel.RWIPosting{postingOf(t, "word-a", "url-a")},
	)
	if !errors.Is(err, yagoproto.ErrBadField) {
		t.Fatalf("Send error = %v, want %v", err, yagoproto.ErrBadField)
	}
	if receipt.State != HandoffRWIRejected || writer.rwiCalls != 1 || writer.urlCalls != 0 {
		t.Fatalf("receipt/writer = %#v/%#v", receipt, writer)
	}
}

func TestHandoffStopsWhenPeerRejectsRWI(t *testing.T) {
	t.Parallel()

	writer := &recordingWriter{
		rwiResponse: yagoproto.TransferRWIResponse{Result: yagoproto.ResultBusy, Pause: 9},
	}
	urls := &recordingURLDirectory{}

	receipt, err := NewHandoff(writer, urls).Send(
		context.Background(),
		peerSeed(t),
		[]yagomodel.RWIPosting{postingOf(t, "word-a", "url-a")},
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

	unknown := []yagomodel.Hash{hashOf(t, "url-a"), hashOf(t, "url-b")}
	rows := []yagomodel.URIMetadataRow{rowOf(t, "url-a"), rowOf(t, "url-b")}
	writer := &recordingWriter{
		rwiResponse: yagoproto.TransferRWIResponse{
			Result:                 yagoproto.ResultOK,
			UnknownURL:             unknown,
			UnknownURLFieldPresent: true,
		},
		urlResponse: yagoproto.TransferURLResponse{
			Result: yagoproto.TransferURLResult(yagoproto.ResultOK),
			Double: 1,
		},
	}
	urls := &recordingURLDirectory{rows: rows}

	receipt, err := NewHandoff(writer, urls).Send(
		context.Background(),
		peerSeed(t),
		[]yagomodel.RWIPosting{postingOf(t, "word-a", "url-a")},
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
		rwiResponse: yagoproto.TransferRWIResponse{
			Result:                 yagoproto.ResultOK,
			UnknownURL:             []yagomodel.Hash{hashOf(t, "url-a")},
			UnknownURLFieldPresent: true,
		},
		urlResponse: yagoproto.TransferURLResponse{
			Result: yagoproto.TransferURLResult(yagoproto.ResultOK),
		},
	}
	urls := &recordingURLDirectory{}

	receipt, err := NewHandoff(writer, urls).Send(
		context.Background(),
		peerSeed(t),
		[]yagomodel.RWIPosting{postingOf(t, "word-a", "url-a")},
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
		rwiResponse: yagoproto.TransferRWIResponse{
			Result:                 yagoproto.ResultOK,
			UnknownURL:             []yagomodel.Hash{rejected},
			UnknownURLFieldPresent: true,
		},
		urlResponse: yagoproto.TransferURLResponse{
			Result:   yagoproto.ResultErrorNotGranted,
			ErrorURL: []yagomodel.Hash{rejected},
		},
	}
	urls := &recordingURLDirectory{rows: []yagomodel.URIMetadataRow{rowOf(t, "url-a")}}

	receipt, err := NewHandoff(writer, urls).Send(
		context.Background(),
		peerSeed(t),
		[]yagomodel.RWIPosting{postingOf(t, "word-a", "url-a")},
	)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	if receipt.State != HandoffURLRejected ||
		!reflect.DeepEqual(receipt.URL.ErrorURL, []yagomodel.Hash{rejected}) {
		t.Fatalf("receipt = %#v", receipt)
	}
}

func TestHandoffReportsTransferAndLookupErrors(t *testing.T) {
	t.Parallel()

	rwiErr := errors.New("rwi failed")
	_, err := NewHandoff(
		&recordingWriter{rwiErr: rwiErr},
		&recordingURLDirectory{},
	).Send(context.Background(), peerSeed(t), []yagomodel.RWIPosting{postingOf(t, "word-a", "url-a")})
	if !errors.Is(err, rwiErr) {
		t.Fatalf("rwi err = %v, want %v", err, rwiErr)
	}
	malformedRWI := errors.Join(yagoproto.ErrBadField, errors.New("malformed rwi"))
	receipt, err := NewHandoff(
		&recordingWriter{rwiErr: malformedRWI},
		&recordingURLDirectory{},
	).Send(context.Background(), peerSeed(t), []yagomodel.RWIPosting{postingOf(t, "word-a", "url-a")})
	if !errors.Is(err, yagoproto.ErrBadField) || receipt.State != HandoffRWIRejected {
		t.Fatalf("malformed rwi error/receipt = %v/%#v", err, receipt)
	}

	rowErr := errors.New("rows failed")
	_, err = NewHandoff(
		&recordingWriter{
			rwiResponse: yagoproto.TransferRWIResponse{
				Result:                 yagoproto.ResultOK,
				UnknownURL:             []yagomodel.Hash{hashOf(t, "url-a")},
				UnknownURLFieldPresent: true,
			},
		},
		&recordingURLDirectory{err: rowErr},
	).Send(context.Background(), peerSeed(t), []yagomodel.RWIPosting{postingOf(t, "word-a", "url-a")})
	if !errors.Is(err, rowErr) {
		t.Fatalf("row err = %v, want %v", err, rowErr)
	}

	urlErr := errors.New("url failed")
	receipt, err = NewHandoff(
		&recordingWriter{
			rwiResponse: yagoproto.TransferRWIResponse{
				Result:                 yagoproto.ResultOK,
				UnknownURL:             []yagomodel.Hash{hashOf(t, "url-a")},
				UnknownURLFieldPresent: true,
			},
			urlErr: urlErr,
		},
		&recordingURLDirectory{rows: []yagomodel.URIMetadataRow{rowOf(t, "url-a")}},
	).Send(context.Background(), peerSeed(t), []yagomodel.RWIPosting{postingOf(t, "word-a", "url-a")})
	if !errors.Is(err, urlErr) || receipt.SentURLRows != 1 {
		t.Fatalf("url err = %v receipt = %#v", err, receipt)
	}

	malformedURL := errors.Join(yagoproto.ErrBadField, errors.New("malformed url"))
	receipt, err = NewHandoff(
		&recordingWriter{
			rwiResponse: yagoproto.TransferRWIResponse{
				Result:                 yagoproto.ResultOK,
				UnknownURL:             []yagomodel.Hash{hashOf(t, "url-a")},
				UnknownURLFieldPresent: true,
			},
			urlErr: malformedURL,
		},
		&recordingURLDirectory{rows: []yagomodel.URIMetadataRow{rowOf(t, "url-a")}},
	).Send(context.Background(), peerSeed(t), []yagomodel.RWIPosting{postingOf(t, "word-a", "url-a")})
	if !errors.Is(err, yagoproto.ErrBadField) || receipt.State != HandoffURLRejected {
		t.Fatalf("malformed url error/receipt = %v/%#v", err, receipt)
	}
}
