package peeradmission

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
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

func serverSeed(t *testing.T, rawURL string) yacymodel.Seed {
	t.Helper()

	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse server url: %v", err)
	}
	host, portText, err := net.SplitHostPort(parsed.Host)
	if err != nil {
		t.Fatalf("split server host: %v", err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("parse server port: %v", err)
	}

	return callerSeed(t, "peer", host, port)
}

func TestCallerBackPingConfirmsValidQueryResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := yacyproto.QueryResponse{Response: 3}
		_, _ = strings.NewReader(resp.Encode().Encode()).WriteTo(w)
	}))
	defer srv.Close()

	probe := newCallerBackPing(srv.Client())

	if !probe.Reachable(
		context.Background(),
		serverSeed(t, srv.URL),
		hashFor("self"),
		"freeworld",
	) {
		t.Fatal("Reachable = false, want true for a confirming caller")
	}
}

func TestCallerBackPingRejectsErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	probe := newCallerBackPing(srv.Client())

	if probe.Reachable(context.Background(), serverSeed(t, srv.URL), hashFor("self"), "freeworld") {
		t.Fatal("Reachable = true, want false on error status")
	}
}

func TestCallerBackPingRejectsTransportError(t *testing.T) {
	probe := newCallerBackPing(&http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("transport failed")
		}),
	})

	if probe.Reachable(
		context.Background(),
		callerSeed(t, "peer", "203.0.113.1", 8090),
		hashFor("self"),
		"freeworld",
	) {
		t.Fatal("Reachable = true, want false on transport error")
	}
}

func TestCallerBackPingRejectsReadError(t *testing.T) {
	probe := newCallerBackPing(&http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       failingBody{readErr: errors.New("read failed")},
			}, nil
		}),
	})

	if probe.Reachable(
		context.Background(),
		callerSeed(t, "peer", "203.0.113.1", 8090),
		hashFor("self"),
		"freeworld",
	) {
		t.Fatal("Reachable = true, want false on read error")
	}
}

func TestCallerBackPingRejectsMessageParseError(t *testing.T) {
	saved := parseBackPingMessage
	t.Cleanup(func() { parseBackPingMessage = saved })
	parseBackPingMessage = func(string) (yacymodel.Message, error) {
		return nil, errors.New("parse failed")
	}
	probe := newCallerBackPing(&http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("response=3\n")),
			}, nil
		}),
	})

	if probe.Reachable(
		context.Background(),
		callerSeed(t, "peer", "203.0.113.1", 8090),
		hashFor("self"),
		"freeworld",
	) {
		t.Fatal("Reachable = true, want false on parse error")
	}
}

func TestCallerBackPingRejectsBadQueryResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = strings.NewReader(yacymodel.Message{yacyproto.FieldResponse: "bad"}.Encode()).
			WriteTo(w)
	}))
	defer srv.Close()

	probe := newCallerBackPing(srv.Client())

	if probe.Reachable(context.Background(), serverSeed(t, srv.URL), hashFor("self"), "freeworld") {
		t.Fatal("Reachable = true, want false on bad query response")
	}
}

func TestCallerBackPingLogsCloseError(t *testing.T) {
	newCallerBackPing(http.DefaultClient).close(
		context.Background(),
		failingBody{closeErr: errors.New("close failed")},
	)
}

func TestCallerBackPingRejectsUnaddressableSeed(t *testing.T) {
	probe := newCallerBackPing(http.DefaultClient)

	if probe.Reachable(
		context.Background(),
		callerSeed(t, "peer", "", 0),
		hashFor("self"),
		"freeworld",
	) {
		t.Fatal("Reachable = true, want false for a seed without an address")
	}
}
