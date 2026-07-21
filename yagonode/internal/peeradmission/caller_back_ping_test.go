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
	"time"

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

func serverSeed(t *testing.T, rawURL string) yagomodel.Seed {
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
		resp := yagoproto.QueryResponse{Response: 3}
		_, _ = strings.NewReader(resp.Encode().Encode()).WriteTo(w)
	}))
	defer srv.Close()

	probe := newCallerBackPing(srv.Client(), false)

	if !probe.Reachable(
		context.Background(),
		serverSeed(t, srv.URL),
		hashFor("self"),
		"freeworld",
	) {
		t.Fatal("Reachable = false, want true for a confirming caller")
	}
}

func TestCallerBackPingSignsControlledNetworkRequest(t *testing.T) {
	self := hashFor("self")
	access := yagoproto.NetworkAccess{
		NetworkName: "private",
		Mode:        yagoproto.NetworkAuthenticationSaltedMagic,
		Essentials:  "shared-secret",
	}
	server := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
			verifier := access
			verifier.Self = self
			if !verifier.Authorizes(request.URL.Query()) ||
				request.URL.Query().Get(yagoproto.FieldIam) != self.String() {
				http.Error(w, "unauthorized", http.StatusUnauthorized)

				return
			}
			response := yagoproto.QueryResponse{Response: 3}
			_, _ = strings.NewReader(response.Encode().Encode()).WriteTo(w)
		}),
	)
	defer server.Close()

	probe := newCallerBackPing(server.Client(), false, access)
	if !probe.Reachable(t.Context(), serverSeed(t, server.URL), self, "private") {
		t.Fatal("controlled caller back-ping was not reachable")
	}
}

func TestCallerBackPingRejectsSigningFailure(t *testing.T) {
	probe := newCallerBackPing(
		&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			t.Fatal("signing failure reached HTTP transport")

			return nil, nil
		})},
		false,
		yagoproto.NetworkAccess{Mode: yagoproto.NetworkAuthenticationSaltedMagic},
	)
	probe.signForm = func(yagoproto.NetworkAccess, url.Values) error {
		return errors.New("signing failed")
	}
	if probe.Reachable(
		t.Context(),
		callerSeed(t, "peer", "203.0.113.1", 8090),
		hashFor("self"),
		"private",
	) {
		t.Fatal("signing failure was reported reachable")
	}
}

func TestCallerBackPingRejectsErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	probe := newCallerBackPing(srv.Client(), false)

	if probe.Reachable(context.Background(), serverSeed(t, srv.URL), hashFor("self"), "freeworld") {
		t.Fatal("Reachable = true, want false on error status")
	}
}

func TestCallerBackPingRejectsTransportError(t *testing.T) {
	probe := newCallerBackPing(&http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("transport failed")
		}),
	}, false)

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
	}, false)

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
	parseBackPingMessage = func(string) (yagomodel.Message, error) {
		return nil, errors.New("parse failed")
	}
	probe := newCallerBackPing(&http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("response=3\n")),
			}, nil
		}),
	}, false)

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
		_, _ = strings.NewReader(yagomodel.Message{yagoproto.FieldResponse: "bad"}.Encode()).
			WriteTo(w)
	}))
	defer srv.Close()

	probe := newCallerBackPing(srv.Client(), false)

	if probe.Reachable(context.Background(), serverSeed(t, srv.URL), hashFor("self"), "freeworld") {
		t.Fatal("Reachable = true, want false on bad query response")
	}
}

func TestCallerBackPingRejectsNegativeQueryResponse(t *testing.T) {
	for _, rejected := range []int{yagoproto.QueryResponseRejected, -2} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			response := yagoproto.QueryResponse{Response: rejected}
			_, _ = strings.NewReader(response.Encode().Encode()).WriteTo(w)
		}))
		probe := newCallerBackPing(srv.Client(), false)
		if probe.Reachable(t.Context(), serverSeed(t, srv.URL), hashFor("self"), "freeworld") {
			t.Fatalf("Reachable = true, want false for response %d", rejected)
		}
		srv.Close()
	}
}

func TestCallerBackPingBoundsBlackholedCallback(t *testing.T) {
	requestFinished := make(chan error, 1)
	probe := newCallerBackPing(&http.Client{
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			<-request.Context().Done()
			requestFinished <- request.Context().Err()

			return nil, request.Context().Err()
		}),
	}, false)
	probe.timeout = time.Millisecond
	started := time.Now()
	if probe.Reachable(
		t.Context(),
		callerSeed(t, "peer", "203.0.113.1", 8090),
		hashFor("self"),
		"freeworld",
	) {
		t.Fatal("Reachable = true, want false for a timed-out callback")
	}
	if !errors.Is(<-requestFinished, context.DeadlineExceeded) {
		t.Fatal("callback did not finish at the probe deadline")
	}
	if elapsed := time.Since(started); elapsed >= time.Second {
		t.Fatalf("bounded callback elapsed = %v", elapsed)
	}
}

func TestCallerBackPingUsesCompatibilityBudgets(t *testing.T) {
	tests := []struct {
		preferHTTPS bool
		want        time.Duration
	}{
		{preferHTTPS: false, want: callerBackPingHTTPTimeout},
		{preferHTTPS: true, want: callerBackPingHTTPSTimeout},
	}
	for _, test := range tests {
		var deadline time.Time
		probe := newCallerBackPing(&http.Client{
			Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				deadline, _ = request.Context().Deadline()

				return nil, errors.New("stop after deadline capture")
			}),
		}, test.preferHTTPS)
		started := time.Now()
		if probe.Reachable(
			t.Context(),
			callerSeed(t, "peer", "203.0.113.1", 8090),
			hashFor("self"),
			"freeworld",
		) {
			t.Fatal("deadline capture was reported reachable")
		}
		budget := deadline.Sub(started)
		if budget < test.want-time.Second || budget > test.want+100*time.Millisecond {
			t.Fatalf("probe budget = %v, want %v", budget, test.want)
		}
	}
}

func TestCallerBackPingLogsCloseError(t *testing.T) {
	newCallerBackPing(http.DefaultClient, false).close(
		context.Background(),
		failingBody{closeErr: errors.New("close failed")},
	)
}

func TestCallerBackPingRejectsUnaddressableSeed(t *testing.T) {
	probe := newCallerBackPing(http.DefaultClient, false)

	if probe.Reachable(
		context.Background(),
		callerSeed(t, "peer", "", 0),
		hashFor("self"),
		"freeworld",
	) {
		t.Fatal("Reachable = true, want false for a seed without an address")
	}
}
