package indextransfer

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagoproto"
)

func TestTransferURLUsesMultipartForLargeForm(t *testing.T) {
	row := rowOf(t, "large-document")
	row.Properties[yagomodel.URLMetaColDescription] = yagomodel.EncodeBase64WireForm(
		strings.Repeat("x", 210000),
	)
	called := false
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		called = true
		if r.Method != http.MethodPost || r.URL.Path != yagoproto.PathTransferURL {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if r.ContentLength <= 200000 {
			t.Fatalf("content length = %d", r.ContentLength)
		}
		if got := transferAttemptFields(t, r).Get("url0"); got != row.String() {
			t.Fatal("metadata changed during encoding")
		}

		return successfulTransferResponse(r.URL.Path), nil
	})}
	self := yagomodel.Seed{Hash: hashOf(t, "self")}
	response, err := NewHTTPPeerWriter(client, yagoproto.DefaultNetwork, self, false).
		TransferURL(t.Context(), peerSeed(t), []yagomodel.URIMetadataRow{row})
	if err != nil || !called || response.Result != yagoproto.TransferURLResult(yagoproto.ResultOK) {
		t.Fatalf("called=%v response=%+v error=%v", called, response, err)
	}
}

func TestTransferURLReplaysEqualFormAfterTransportFailure(t *testing.T) {
	row := rowOf(t, "replayed-document")
	row.Properties[yagomodel.URLMetaColDescription] = yagomodel.EncodeBase64WireForm(
		strings.Repeat("x", 210000),
	)
	replayed := formAcrossTransportFailure(t, func(
		ctx context.Context,
		writer HTTPPeerWriter,
		peer yagomodel.Seed,
	) error {
		_, err := writer.TransferURL(ctx, peer, []yagomodel.URIMetadataRow{row})

		return err
	})
	if replayed.Get("url0") != row.String() {
		t.Fatal("replayed metadata row changed")
	}
}

func TestTransferRWIReplaysEqualFormAfterTransportFailure(t *testing.T) {
	replayed := formAcrossTransportFailure(t, func(
		ctx context.Context,
		writer HTTPPeerWriter,
		peer yagomodel.Seed,
	) error {
		_, err := writer.TransferRWI(ctx, peer, []yagomodel.RWIPosting{
			postingOf(t, "word-a", "replayed-document"),
			postingOf(t, "word-a", "second-document"),
		})

		return err
	})
	request, err := yagoproto.ParseTransferRWIRequest(t.Context(), replayed)
	if err != nil || request.EntryCount != 2 || len(request.Indexes) != 2 {
		t.Fatalf("replayed postings = %+v, error = %v", request, err)
	}
}

func formAcrossTransportFailure(t *testing.T, operation transferDeadlineOperation) url.Values {
	t.Helper()
	var attempts []url.Values
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		attempts = append(attempts, transferAttemptFields(t, r))
		if len(attempts) == 1 {
			return nil, errors.New("first address unavailable")
		}

		return successfulTransferResponse(r.URL.Path), nil
	})}
	writer := NewHTTPPeerWriter(
		client,
		yagoproto.DefaultNetwork,
		yagomodel.Seed{Hash: hashOf(t, "self")},
		false,
	)
	if err := operation(t.Context(), writer, multiAddressTransferPeer(t)); err != nil {
		t.Fatalf("transfer after replay: %v", err)
	}
	if len(attempts) != 2 || !reflect.DeepEqual(attempts[0], attempts[1]) {
		t.Fatalf("attempt forms differ across %d attempts", len(attempts))
	}

	return attempts[1]
}

func TestTransferDoesNotRetryHTTPRefusal(t *testing.T) {
	for name, operation := range transferDeadlineOperations(t) {
		t.Run(name, func(t *testing.T) {
			attempts := 0
			client := &http.Client{
				Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
					attempts++

					return &http.Response{
						StatusCode: http.StatusServiceUnavailable,
						Body:       io.NopCloser(strings.NewReader("busy")),
						Header:     make(http.Header),
					}, nil
				}),
			}
			writer := NewHTTPPeerWriter(
				client,
				yagoproto.DefaultNetwork,
				yagomodel.Seed{Hash: hashOf(t, "self")},
				false,
			)
			err := operation(t.Context(), writer, multiAddressTransferPeer(t))
			if !errors.Is(err, errTransferFailed) || errors.Is(err, errTransferTransport) ||
				attempts != 1 {
				t.Fatalf("refused transfer error/attempts = %v/%d", err, attempts)
			}
		})
	}
}

func TestTransferIssuesNoAttemptAfterCancellation(t *testing.T) {
	for name, operation := range transferDeadlineOperations(t) {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			attempts := 0
			client := &http.Client{
				Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
					attempts++
					cancel()

					return nil, r.Context().Err()
				}),
			}
			writer := NewHTTPPeerWriter(
				client,
				yagoproto.DefaultNetwork,
				yagomodel.Seed{Hash: hashOf(t, "self")},
				false,
			)
			err := operation(ctx, writer, multiAddressTransferPeer(t))
			if !errors.Is(err, context.Canceled) || attempts != 1 {
				t.Fatalf("canceled transfer error/attempts = %v/%d", err, attempts)
			}
		})
	}
}

func transferAttemptFields(t *testing.T, r *http.Request) url.Values {
	t.Helper()
	body, err := io.ReadAll(r.Body)
	if err != nil || int64(len(body)) != r.ContentLength {
		t.Fatalf(
			"body length = %d, content length = %d, error = %v",
			len(body),
			r.ContentLength,
			err,
		)
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	parseMultipartForm(t, r)

	return url.Values(r.MultipartForm.Value)
}

func multipartBoundaryOf(tb testing.TB, r *http.Request) string {
	tb.Helper()
	mediaType, parameters, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" || parameters["boundary"] == "" {
		tb.Fatalf("content type = %q, error = %v", r.Header.Get("Content-Type"), err)
	}

	return parameters["boundary"]
}
