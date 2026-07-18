package yagonode

import (
	"context"
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/rwi"
	"github.com/D4rk4/yago/yagonode/internal/urlmeta"
)

type nodeGrowthAdmission struct {
	err              error
	calls            int
	requiredHeadroom uint64
}

func (a *nodeGrowthAdmission) CheckGrowthWithHeadroom(required uint64) error {
	a.requiredHeadroom = required

	return a.CheckGrowth()
}

func (a *nodeGrowthAdmission) CheckGrowth() error {
	a.calls++

	return a.err
}

type recordingPostingGrowthReceiver struct {
	calls int
	err   error
}

func (r *recordingPostingGrowthReceiver) Receive(
	context.Context,
	[]yagomodel.RWIPosting,
) (rwi.Receipt, error) {
	r.calls++

	return rwi.Receipt{}, r.err
}

type recordingURLGrowthReceiver struct {
	calls int
	err   error
}

func (r *recordingURLGrowthReceiver) Receive(
	context.Context,
	[]yagomodel.URIMetadataRow,
) (urlmeta.Receipt, error) {
	r.calls++

	return urlmeta.Receipt{}, r.err
}

func TestPeerGrowthReceiversReturnBusyUnderPressure(t *testing.T) {
	pressure := &nodeGrowthAdmission{err: errors.New("pressure")}
	postings := &recordingPostingGrowthReceiver{}
	postingReceiver := admittedPostingReceiver{inner: postings, admission: pressure}
	if receipt, err := postingReceiver.Receive(
		t.Context(),
		[]yagomodel.RWIPosting{{}},
	); err != nil || !receipt.Busy || receipt.Pause != 30_000 {
		t.Fatalf("posting pressure receipt = %+v, %v", receipt, err)
	}
	urls := &recordingURLGrowthReceiver{}
	urlReceiver := admittedURLReceiver{inner: urls, admission: pressure}
	if receipt, err := urlReceiver.Receive(
		t.Context(),
		[]yagomodel.URIMetadataRow{{}},
	); err != nil ||
		!receipt.Busy {
		t.Fatalf("url pressure receipt = %+v, %v", receipt, err)
	}
	if postings.calls != 0 || urls.calls != 0 {
		t.Fatal("pressure reached inner peer receiver")
	}
	if _, err := postingReceiver.Receive(t.Context(), nil); err != nil {
		t.Fatalf("empty posting receive: %v", err)
	}
	if _, err := urlReceiver.Receive(t.Context(), nil); err != nil {
		t.Fatalf("empty url receive: %v", err)
	}
	pressure.err = nil
	if _, err := postingReceiver.Receive(t.Context(), []yagomodel.RWIPosting{{}}); err != nil {
		t.Fatalf("healthy posting receive: %v", err)
	}
	if _, err := urlReceiver.Receive(t.Context(), []yagomodel.URIMetadataRow{{}}); err != nil {
		t.Fatalf("healthy url receive: %v", err)
	}
	if postings.calls != 2 || urls.calls != 2 {
		t.Fatalf("inner calls = postings:%d urls:%d", postings.calls, urls.calls)
	}
}

func TestStorageGrowthAdmissionDecoratorIsOptional(t *testing.T) {
	postings := &recordingPostingGrowthReceiver{}
	urls := &recordingURLGrowthReceiver{}
	storage := nodeStorage{postingReceiver: postings, urlReceiver: urls}
	if got := storageWithGrowthAdmission(storage, nil); got.postingReceiver != postings ||
		got.urlReceiver != urls {
		t.Fatal("nil growth admission changed storage receivers")
	}
	admission := &nodeGrowthAdmission{}
	got := storageWithGrowthAdmission(storage, admission)
	if _, ok := got.postingReceiver.(admittedPostingReceiver); !ok {
		t.Fatalf("posting receiver type = %T", got.postingReceiver)
	}
	if _, ok := got.urlReceiver.(admittedURLReceiver); !ok {
		t.Fatalf("url receiver type = %T", got.urlReceiver)
	}
}

func TestPeerGrowthReceiversWrapInnerFailures(t *testing.T) {
	postingFailure := errors.New("posting failed")
	postingReceiver := admittedPostingReceiver{
		inner: &recordingPostingGrowthReceiver{err: postingFailure},
	}
	if _, err := postingReceiver.Receive(t.Context(), nil); !errors.Is(err, postingFailure) {
		t.Fatalf("posting receiver error = %v", err)
	}
	urlFailure := errors.New("URL metadata failed")
	urlReceiver := admittedURLReceiver{
		inner: &recordingURLGrowthReceiver{err: urlFailure},
	}
	if _, err := urlReceiver.Receive(t.Context(), nil); !errors.Is(err, urlFailure) {
		t.Fatalf("URL receiver error = %v", err)
	}
}
