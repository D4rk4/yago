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
				callerSeed(t, "server", "203.0.113.1"),
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

func TestPeerGreeterSignsControlledNetworkRequest(t *testing.T) {
	self := callerSeed(t, "self", "203.0.113.9")
	access := yagoproto.NetworkAccess{
		NetworkName: "private",
		Mode:        yagoproto.NetworkAuthenticationSaltedMagic,
		Essentials:  "shared-secret",
	}
	server := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
			if err := request.ParseForm(); err != nil {
				t.Fatal(err)
			}
			verifier := access
			verifier.Self = self.Hash
			if !verifier.Authorizes(request.PostForm) ||
				request.PostForm.Get(yagoproto.FieldIam) != self.Hash.String() {
				http.Error(w, "unauthorized", http.StatusUnauthorized)

				return
			}
			response := yagoproto.HelloResponse{
				YourIP: "203.0.113.9", YourType: yagomodel.PeerSenior,
				Seeds: []yagomodel.Seed{callerSeed(t, "server", "203.0.113.1")},
			}
			_, _ = strings.NewReader(response.Encode().Encode()).WriteTo(w)
		}),
	)
	defer server.Close()

	result, err := newHTTPPeerGreeter(server.Client(), "private", false, access).Greet(
		t.Context(),
		serverSeed(t, server),
		self,
		0,
	)
	if err != nil || result.YourType != yagomodel.PeerSenior {
		t.Fatalf("controlled greet = %+v, %v", result, err)
	}
}

func TestPeerGreeterSurfacesSigningFailure(t *testing.T) {
	sentinel := errors.New("signing failed")
	greeter := newHTTPPeerGreeter(
		http.DefaultClient,
		"private",
		false,
		yagoproto.NetworkAccess{Mode: yagoproto.NetworkAuthenticationSaltedMagic},
	)
	greeter.signForm = func(yagoproto.NetworkAccess, url.Values) error { return sentinel }
	_, err := greeter.Greet(
		t.Context(),
		callerSeed(t, "target", "203.0.113.1"),
		callerSeed(t, "self", "203.0.113.9"),
		0,
	)
	if !errors.Is(err, sentinel) || !errors.Is(err, errGreetFailed) {
		t.Fatalf("greet signing error = %v", err)
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

func TestPeerGreeterValidatesRequiredHelloResponseFields(t *testing.T) {
	responder := callerSeed(t, "server", "203.0.113.1")
	for _, test := range []struct {
		name     string
		yourIP   string
		yourType yagomodel.PeerType
		seeds    []yagomodel.Seed
	}{
		{name: "missing caller address", yourType: yagomodel.PeerSenior, seeds: []yagomodel.Seed{responder}},
		{name: "invalid caller address", yourIP: "public.example", yourType: yagomodel.PeerSenior, seeds: []yagomodel.Seed{responder}},
		{name: "unspecified caller address", yourIP: "0.0.0.0", yourType: yagomodel.PeerSenior, seeds: []yagomodel.Seed{responder}},
		{name: "invalid caller type", yourIP: "203.0.113.9", yourType: yagomodel.PeerMentor, seeds: []yagomodel.Seed{responder}},
		{name: "missing responder seed", yourIP: "203.0.113.9", yourType: yagomodel.PeerSenior},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := yagoproto.HelloResponse{
				YourIP: test.yourIP, YourType: test.yourType, Seeds: test.seeds,
			}
			if _, err := parseGreetResponse(
				t.Context(),
				strings.NewReader(response.Encode().Encode()),
			); err == nil {
				t.Fatal("invalid hello response was accepted")
			}
		})
	}
}

func TestPeerGreeterAcceptsCommaSeparatedCallerAddresses(t *testing.T) {
	response := yagoproto.HelloResponse{
		YourIP:   "203.0.113.9, 2001:db8::9",
		YourType: yagomodel.PeerSenior,
		Seeds:    []yagomodel.Seed{callerSeed(t, "server", "203.0.113.1")},
	}
	result, err := parseGreetResponse(
		t.Context(),
		strings.NewReader(response.Encode().Encode()),
	)
	if err != nil || result.YourIP != response.YourIP {
		t.Fatalf("comma-separated caller addresses = %+v, %v", result, err)
	}
}

func TestPeerGreeterRejectsMismatchedResponderIdentity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		response := yagoproto.HelloResponse{
			YourIP:   "203.0.113.9",
			YourType: yagomodel.PeerSenior,
			Seeds:    []yagomodel.Seed{callerSeed(t, "different", "203.0.113.1")},
		}
		_, _ = strings.NewReader(response.Encode().Encode()).WriteTo(w)
	}))
	defer server.Close()

	_, err := newHTTPPeerGreeter(server.Client(), "freeworld", false).Greet(
		t.Context(),
		serverSeed(t, server),
		callerSeed(t, "self", "203.0.113.9"),
		0,
	)
	if !errors.Is(err, errGreetFailed) {
		t.Fatalf("mismatched responder error = %v", err)
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
