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

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagoproto"
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

func serverSeed(t *testing.T, server *httptest.Server) yagomodel.Seed {
	t.Helper()

	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server url: %v", err)
	}
	host, err := yagomodel.ParseHost(parsed.Hostname())
	if err != nil {
		t.Fatalf("parse server host: %v", err)
	}
	port, err := yagomodel.ParsePort(parsed.Port())
	if err != nil {
		t.Fatalf("parse server port: %v", err)
	}

	return yagomodel.Seed{
		Hash: hashFor("server"),
		IP:   yagomodel.Some(host),
		Port: yagomodel.Some(port),
	}
}

func TestPeerGreeterLearnsTypeAndKnownSeeds(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		resp := yagoproto.HelloResponse{
			YourIP:   "203.0.113.9",
			YourType: yagomodel.PeerSenior,
			Seeds: []yagomodel.Seed{
				callerSeed(t, "self", "203.0.113.9"),
				callerSeed(t, "known", "198.51.100.7"),
			},
		}
		_, _ = strings.NewReader(resp.Encode().Encode()).WriteTo(w)
	}))
	defer server.Close()

	greeter := newHTTPPeerGreeter(server.Client(), "freeworld", false)
	result, err := greeter.Greet(
		context.Background(),
		serverSeed(t, server),
		callerSeed(t, "self", "203.0.113.9"),
		0,
	)
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if gotPath != yagoproto.PathHello {
		t.Errorf("path = %q, want %q", gotPath, yagoproto.PathHello)
	}
	if result.YourType != yagomodel.PeerSenior {
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

	greeter := newHTTPPeerGreeter(server.Client(), "freeworld", false)
	if _, err := greeter.Greet(
		context.Background(),
		serverSeed(t, server),
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

	greeter := newHTTPPeerGreeter(http.DefaultClient, "freeworld", false)
	if _, err := greeter.Greet(
		context.Background(),
		callerSeed(t, "target", "203.0.113.1"),
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
	}, "freeworld", false)

	if _, err := greeter.Greet(
		context.Background(),
		callerSeed(t, "target", "203.0.113.1"),
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
	}, "freeworld", false)

	if _, err := greeter.Greet(
		context.Background(),
		callerSeed(t, "target", "203.0.113.1"),
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
	parseGreetMessage = func(string) (yagomodel.Message, error) {
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
		strings.NewReader(yagomodel.Message{yagoproto.FieldYourType: "unknown"}.Encode()),
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
	greeter := newHTTPPeerGreeter(http.DefaultClient, "freeworld", false)
	if _, err := greeter.Greet(
		context.Background(),
		yagomodel.Seed{Hash: hashFor("target")},
		callerSeed(t, "self", "203.0.113.9"),
		0,
	); err == nil {
		t.Fatal("expected error for a target without a reachable address")
	}
}
