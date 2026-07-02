package peerannouncement

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yacymodel"
	"github.com/D4rk4/yago/yacyproto"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type failingBody struct {
	readErr  error
	closeErr error
}

func (b failingBody) Read([]byte) (int, error) {
	return 0, b.readErr
}

func (b failingBody) Close() error {
	return b.closeErr
}

func endpointOf(t *testing.T, server *httptest.Server) string {
	t.Helper()

	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server url: %v", err)
	}

	return parsed.Host
}

func TestPeerGreeterLearnsTypeAndKnownSeeds(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		resp := yacyproto.HelloResponse{
			YourIP:   "203.0.113.9",
			YourType: yacymodel.PeerSenior,
			Seeds: []yacymodel.Seed{
				callerSeed(t, "self", "203.0.113.9"),
				callerSeed(t, "known", "198.51.100.7"),
			},
		}
		_, _ = strings.NewReader(resp.Encode().Encode()).WriteTo(w)
	}))
	defer server.Close()

	greeter := newHTTPPeerGreeter(server.Client(), "freeworld")
	result, err := greeter.Greet(
		context.Background(),
		endpointOf(t, server),
		callerSeed(t, "self", "203.0.113.9"),
		0,
	)
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if gotPath != yacyproto.PathHello {
		t.Errorf("path = %q, want %q", gotPath, yacyproto.PathHello)
	}
	if result.YourType != yacymodel.PeerSenior {
		t.Errorf("type = %v, want senior", result.YourType)
	}
	if len(result.Known) != 1 {
		t.Fatalf("known = %d, want 1 (own seed excluded)", len(result.Known))
	}
}

func TestPeerGreeterRejectsNon200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	greeter := newHTTPPeerGreeter(server.Client(), "freeworld")
	if _, err := greeter.Greet(
		context.Background(),
		endpointOf(t, server),
		callerSeed(t, "self", "203.0.113.9"),
		0,
	); err == nil {
		t.Fatal("expected error on non-200")
	}
}

func TestPeerGreeterRejectsRequestCreationError(t *testing.T) {
	saved := newGreetRequest
	t.Cleanup(func() { newGreetRequest = saved })
	sentinel := errors.New("request failed")
	newGreetRequest = func(context.Context, string, string, io.Reader) (*http.Request, error) {
		return nil, sentinel
	}

	greeter := newHTTPPeerGreeter(http.DefaultClient, "freeworld")
	if _, err := greeter.Greet(
		context.Background(),
		"203.0.113.1:8090",
		callerSeed(t, "self", "203.0.113.9"),
		0,
	); !errors.Is(err, sentinel) {
		t.Fatalf("Greet error = %v, want %v", err, sentinel)
	}
}

func TestPeerGreeterRejectsTransportError(t *testing.T) {
	sentinel := errors.New("transport failed")
	greeter := newHTTPPeerGreeter(&http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, sentinel
		}),
	}, "freeworld")

	if _, err := greeter.Greet(
		context.Background(),
		"203.0.113.1:8090",
		callerSeed(t, "self", "203.0.113.9"),
		0,
	); !errors.Is(err, sentinel) {
		t.Fatalf("Greet error = %v, want %v", err, sentinel)
	}
}

func TestPeerGreeterRejectsReadError(t *testing.T) {
	sentinel := errors.New("read failed")
	greeter := newHTTPPeerGreeter(&http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       failingBody{readErr: sentinel},
			}, nil
		}),
	}, "freeworld")

	if _, err := greeter.Greet(
		context.Background(),
		"203.0.113.1:8090",
		callerSeed(t, "self", "203.0.113.9"),
		0,
	); !errors.Is(err, sentinel) {
		t.Fatalf("Greet error = %v, want %v", err, sentinel)
	}
}

func TestPeerGreeterRejectsMessageParseError(t *testing.T) {
	saved := parseGreetMessage
	t.Cleanup(func() { parseGreetMessage = saved })
	sentinel := errors.New("parse failed")
	parseGreetMessage = func(string) (yacymodel.Message, error) {
		return nil, sentinel
	}

	_, err := parseGreetResponse(context.Background(), strings.NewReader("response=3\n"))
	if !errors.Is(err, sentinel) {
		t.Fatalf("parseGreetResponse error = %v, want %v", err, sentinel)
	}
}

func TestPeerGreeterRejectsBadHelloResponse(t *testing.T) {
	_, err := parseGreetResponse(
		context.Background(),
		strings.NewReader(yacymodel.Message{yacyproto.FieldYourType: "unknown"}.Encode()),
	)
	if err == nil {
		t.Fatal("expected bad hello response error")
	}
}

func TestPeerGreeterLogsCloseError(t *testing.T) {
	closeResponseBody(
		context.Background(),
		failingBody{closeErr: errors.New("close failed")},
		"test",
	)
}

func TestPeerGreeterRejectsEmptyEndpoint(t *testing.T) {
	greeter := newHTTPPeerGreeter(http.DefaultClient, "freeworld")
	if _, err := greeter.Greet(
		context.Background(),
		"  ",
		callerSeed(t, "self", "203.0.113.9"),
		0,
	); err == nil {
		t.Fatal("expected error for empty endpoint")
	}
}
