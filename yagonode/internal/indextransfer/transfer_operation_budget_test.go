package indextransfer

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagoproto"
)

type transferDeadlineOperation func(context.Context, HTTPPeerWriter, yagomodel.Seed) error

type transferDeadlineCase struct {
	clientTimeout  time.Duration
	callerTimeout  time.Duration
	operationLimit time.Duration
}

func TestMultiAddressTransfersShareOneOperationDeadline(t *testing.T) {
	for caseName, testCase := range transferDeadlineCases() {
		for operationName, operation := range transferDeadlineOperations(t) {
			t.Run(caseName+" "+operationName, func(t *testing.T) {
				verifySharedTransferDeadline(t, testCase, operation)
			})
		}
	}
}

func transferDeadlineOperations(t *testing.T) map[string]transferDeadlineOperation {
	return map[string]transferDeadlineOperation{
		"rwi": func(ctx context.Context, writer HTTPPeerWriter, peer yagomodel.Seed) error {
			_, err := writer.TransferRWI(
				ctx,
				peer,
				[]yagomodel.RWIPosting{postingOf(t, "word", "url")},
			)

			return err
		},
		"url": func(ctx context.Context, writer HTTPPeerWriter, peer yagomodel.Seed) error {
			_, err := writer.TransferURL(ctx, peer, []yagomodel.URIMetadataRow{rowOf(t, "url")})

			return err
		},
	}
}

func transferDeadlineCases() map[string]transferDeadlineCase {
	return map[string]transferDeadlineCase{
		"client deadline": {
			clientTimeout:  4 * time.Second,
			operationLimit: 4 * time.Second,
		},
		"earlier caller deadline": {
			clientTimeout:  8 * time.Second,
			callerTimeout:  2 * time.Second,
			operationLimit: 2 * time.Second,
		},
	}
}

func verifySharedTransferDeadline(
	t *testing.T,
	testCase transferDeadlineCase,
	operation transferDeadlineOperation,
) {
	started := time.Now()
	deadlines := make([]time.Time, 0, 2)
	calls := 0
	client := &http.Client{
		Timeout: testCase.clientTimeout,
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			deadline, bounded := request.Context().Deadline()
			if !bounded {
				t.Fatal("transfer attempt has no deadline")
			}
			deadlines = append(deadlines, deadline)
			calls++
			if calls == 1 {
				return nil, errors.New("first address unavailable")
			}

			return successfulTransferResponse(request.URL.Path), nil
		}),
	}
	ctx := context.Background()
	if testCase.callerTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, testCase.callerTimeout)
		defer cancel()
	}
	writer := NewHTTPPeerWriter(
		client,
		yagoproto.DefaultNetwork,
		yagomodel.Seed{Hash: hashOf(t, "self")},
		false,
	)

	if err := operation(ctx, writer, multiAddressTransferPeer(t)); err != nil {
		t.Fatalf("transfer: %v", err)
	}
	if len(deadlines) != 2 {
		t.Fatalf("attempt deadlines = %v, want two", deadlines)
	}
	firstAllocation := deadlines[0].Sub(started)
	if firstAllocation <= 0 || firstAllocation >= testCase.operationLimit*3/4 {
		t.Fatalf(
			"first attempt allocation = %s, want a bounded share of %s",
			firstAllocation,
			testCase.operationLimit,
		)
	}
	if deadlines[1].After(started.Add(testCase.operationLimit + 250*time.Millisecond)) {
		t.Fatalf(
			"last attempt deadline = %s, exceeds operation limit %s",
			deadlines[1].Sub(started),
			testCase.operationLimit,
		)
	}
}

func multiAddressTransferPeer(t *testing.T) yagomodel.Seed {
	t.Helper()

	primary, err := yagomodel.ParseHost("192.0.2.1")
	if err != nil {
		t.Fatal(err)
	}
	alternatives, err := yagomodel.ParseIP6("198.51.100.1")
	if err != nil {
		t.Fatal(err)
	}

	return yagomodel.Seed{
		Hash: hashOf(t, "peer"),
		IP:   yagomodel.Some(primary),
		IP6:  yagomodel.Some(alternatives),
		Port: yagomodel.Some(yagomodel.Port(8090)),
	}
}

func successfulTransferResponse(path string) *http.Response {
	body := yagoproto.TransferURLResponse{
		Result: yagoproto.TransferURLResult(yagoproto.ResultOK),
	}.Encode()
	if path == yagoproto.PathTransferRWI {
		body = yagoproto.TransferRWIResponse{
			Result: yagoproto.TransferRWIResult(yagoproto.ResultOK),
		}.Encode()
	}

	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body.Encode())),
	}
}
