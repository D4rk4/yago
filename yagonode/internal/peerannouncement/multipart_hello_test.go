package peerannouncement

import (
	"bytes"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/peerwire"
	"github.com/D4rk4/yago/yagoproto"
)

func TestPeerGreeterReplaysEqualHelloFormAfterTransportFailure(t *testing.T) {
	target := callerSeed(t, "target", "192.0.2.7")
	alternative, err := yagomodel.ParseIP6("2001:db8::7")
	if err != nil {
		t.Fatal(err)
	}
	target.IP6 = yagomodel.Some(alternative)
	self := callerSeed(t, "self", "203.0.113.9")
	response := yagoproto.HelloResponse{
		YourIP:   "203.0.113.9",
		YourType: yagomodel.PeerSenior,
		Seeds:    []yagomodel.Seed{target},
	}.Encode().Encode()
	var attempts []url.Values
	client := &http.Client{
		Timeout: time.Second,
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			attempts = append(attempts, helloAttemptFields(t, request))
			if len(attempts) == 1 {
				return nil, errors.New("primary unavailable")
			}

			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(response)),
			}, nil
		}),
	}

	if _, err := newHTTPPeerGreeter(client, "freeworld", false).Greet(
		t.Context(),
		target,
		self,
		0,
		yagomodel.None[peerwire.Endpoint](),
	); err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 2 || !reflect.DeepEqual(attempts[0], attempts[1]) {
		t.Fatalf("hello forms differ across %d attempts", len(attempts))
	}
	if attempts[1].Get(yagoproto.FieldIam) != self.Hash.String() ||
		attempts[1].Get(yagoproto.FieldNetworkName) != "freeworld" ||
		attempts[1].Get(yagoproto.FieldSeed) == "" {
		t.Fatalf("replayed hello form = %q", attempts[1])
	}
}

func helloAttemptFields(t *testing.T, request *http.Request) url.Values {
	t.Helper()
	mediaType, parameters, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" || parameters["boundary"] == "" {
		t.Fatalf("content type = %q, error = %v", request.Header.Get("Content-Type"), err)
	}
	body, err := io.ReadAll(request.Body)
	if err != nil || int64(len(body)) != request.ContentLength {
		t.Fatalf(
			"body length = %d, content length = %d, error = %v",
			len(body),
			request.ContentLength,
			err,
		)
	}
	request.Body = io.NopCloser(bytes.NewReader(body))
	if err := request.ParseMultipartForm(1 << 20); err != nil {
		t.Fatalf("parse multipart hello: %v", err)
	}
	t.Cleanup(func() { _ = request.MultipartForm.RemoveAll() })

	return url.Values(request.MultipartForm.Value)
}
