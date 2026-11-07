package peeradmission

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/peerwire"
	"github.com/D4rk4/yago/yagoproto"
)

func (s *stubReachability) AnsweredEndpoint(
	_ context.Context,
	peer yagomodel.Hash,
) (peerwire.Endpoint, bool, error) {
	endpoint, found := s.answered[peer]

	return endpoint, found, s.answeredErr
}

func (s *stubReachability) ObserveAnsweredCaller(
	ctx context.Context,
	caller yagomodel.Seed,
	classification yagomodel.PeerType,
	endpoint peerwire.Endpoint,
) {
	s.ObserveCaller(ctx, caller, classification)
	if s.remembered == nil {
		s.remembered = map[yagomodel.Hash]peerwire.Endpoint{}
	}
	s.remembered[caller.Hash] = endpoint
}

type backPingAnswer func(*url.URL) (*http.Response, error)

func backPingResponse(response int) *http.Response {
	body := yagoproto.QueryResponse{Response: response}.Encode().Encode()

	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}
}

func recordingBackPing(preferHTTPS bool, answer backPingAnswer) (callerBackPing, *[]string) {
	attempts := []string{}
	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			attempts = append(attempts, peerwire.EndpointAt(request.URL).Origin())

			return answer(request.URL)
		}),
	}

	return newCallerBackPing(client, preferHTTPS), &attempts
}

func answeredCallerOrigin(t *testing.T, origin string) peerwire.Endpoint {
	t.Helper()
	endpoint, err := peerwire.ParseEndpointOrigin(origin)
	if err != nil {
		t.Fatal(err)
	}

	return endpoint
}

func dualStackCaller(t *testing.T) yagomodel.Seed {
	t.Helper()
	caller := callerSeed(t, "caller", "192.0.2.7", 8090)
	alternatives, err := yagomodel.ParseIP6("2001:db8::7")
	if err != nil {
		t.Fatal(err)
	}
	caller.IP6 = yagomodel.Some(alternatives)

	return caller
}

func pingCaller(
	ctx context.Context,
	probe callerBackPing,
	caller yagomodel.Seed,
	answered yagomodel.Optional[peerwire.Endpoint],
) yagomodel.Optional[peerwire.Endpoint] {
	_, contacted := probe.ReachableCaller(ctx, caller, hashFor("self"), "freeworld", answered)

	return contacted
}

func assertBackPingAttempts(t *testing.T, attempts *[]string, want ...string) {
	t.Helper()
	if !slices.Equal(*attempts, want) {
		t.Fatalf("back-ping attempts = %v, want %v", *attempts, want)
	}
}

func TestBackPingTriesAnsweredEndpointFirstAndReportsExactContact(t *testing.T) {
	caller := dualStackCaller(t)
	answered := answeredCallerOrigin(t, "http://[2001:db8::7]:8090")
	probe, attempts := recordingBackPing(false, func(*url.URL) (*http.Response, error) {
		return backPingResponse(3), nil
	})

	contacted := pingCaller(t.Context(), probe, caller, yagomodel.Some(answered))

	if contacted != yagomodel.Some(answered) {
		t.Fatalf("contacted = %+v, want %+v", contacted, answered)
	}
	assertBackPingAttempts(t, attempts, "http://[2001:db8::7]:8090")
}

func TestBackPingAddsImplicitHTTPSOnlyForDNSNamesUnderPreference(t *testing.T) {
	named := callerSeed(t, "caller", "example.org", 8090)
	cases := []struct {
		caller      yagomodel.Seed
		preferHTTPS bool
		want        []string
	}{
		{named, true, []string{"https://example.org:443", "http://example.org:8090"}},
		{named, false, []string{"http://example.org:8090"}},
		{dualStackCaller(t), true, []string{"http://192.0.2.7:8090", "http://[2001:db8::7]:8090"}},
	}
	for _, tc := range cases {
		probe, attempts := recordingBackPing(
			tc.preferHTTPS,
			func(*url.URL) (*http.Response, error) {
				return nil, errors.New("endpoint unavailable")
			},
		)
		if contacted := pingCaller(
			t.Context(),
			probe,
			tc.caller,
			yagomodel.None[peerwire.Endpoint](),
		); contacted.Present() {
			t.Fatalf("unavailable caller contacted at %+v", contacted)
		}
		assertBackPingAttempts(t, attempts, tc.want...)
	}

	probe, attempts := recordingBackPing(true, func(target *url.URL) (*http.Response, error) {
		if target.Scheme != "https" {
			return nil, errors.New("endpoint unavailable")
		}

		return backPingResponse(3), nil
	})
	contacted := pingCaller(t.Context(), probe, named, yagomodel.None[peerwire.Endpoint]())
	if contacted != yagomodel.Some(answeredCallerOrigin(t, "https://example.org:443")) {
		t.Fatalf("implicit HTTPS contact = %+v", contacted)
	}
	assertBackPingAttempts(t, attempts, "https://example.org:443")
}

func TestBackPingReportsOnlyTheEndpointThatConfirmsCaller(t *testing.T) {
	caller := dualStackCaller(t)
	for name, primaryAnswer := range map[string]backPingAnswer{
		"wrong identity": func(*url.URL) (*http.Response, error) {
			return backPingResponse(-1), nil
		},
		"malformed": func(*url.URL) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("not a query response")),
			}, nil
		},
		"redirect": func(*url.URL) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusFound,
				Header:     http.Header{"Location": {"http://198.51.100.9:8090/yacy/query.html"}},
				Body:       io.NopCloser(strings.NewReader("")),
			}, nil
		},
		"failed request": func(*url.URL) (*http.Response, error) {
			return nil, errors.New("connection refused")
		},
	} {
		probe, attempts := recordingBackPing(false, func(target *url.URL) (*http.Response, error) {
			if target.Hostname() == "192.0.2.7" {
				return primaryAnswer(target)
			}

			return backPingResponse(3), nil
		})

		contacted := pingCaller(t.Context(), probe, caller, yagomodel.None[peerwire.Endpoint]())

		if contacted != yagomodel.Some(answeredCallerOrigin(t, "http://[2001:db8::7]:8090")) {
			t.Fatalf("%s: contacted = %+v", name, contacted)
		}
		assertBackPingAttempts(t, attempts, "http://192.0.2.7:8090", "http://[2001:db8::7]:8090")
	}
}

func TestBackPingWithExpiredDeadlineAttemptsNothing(t *testing.T) {
	probe, attempts := recordingBackPing(false, func(*url.URL) (*http.Response, error) {
		return backPingResponse(3), nil
	})
	expired, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer cancel()

	contacted := pingCaller(
		expired,
		probe,
		dualStackCaller(t),
		yagomodel.Some(answeredCallerOrigin(t, "http://[2001:db8::7]:8090")),
	)

	if contacted.Present() {
		t.Fatalf("expired back-ping contacted %+v", contacted)
	}
	assertBackPingAttempts(t, attempts)
}

func TestHelloRemembersEndpointThatConfirmedCaller(t *testing.T) {
	caller := callerSeed(t, "caller", "203.0.113.1", 8090)
	remembered := answeredCallerOrigin(t, "http://203.0.113.1:8090")
	contacted := answeredCallerOrigin(t, "https://203.0.113.1:8443")
	reachability := &stubReachability{
		answered: map[yagomodel.Hash]peerwire.Endpoint{caller.Hash: remembered},
	}
	probe := &stubProbe{reachable: true, contacted: contacted}

	resp, err := newEndpoint(t, probe, reachability).Serve(
		t.Context(), helloRequest("freeworld", caller, 0),
	)

	if err != nil || resp.YourType != yagomodel.PeerSenior {
		t.Fatalf("hello = %q/%v", resp.YourType, err)
	}
	if probe.answered != yagomodel.Some(remembered) {
		t.Fatalf("probe answered endpoint = %+v, want %+v", probe.answered, remembered)
	}
	if reachability.remembered[caller.Hash] != contacted {
		t.Fatalf("remembered = %v, want %+v", reachability.remembered, contacted)
	}
}

func TestHelloRemembersNothingForUnconfirmedCaller(t *testing.T) {
	caller := callerSeed(t, "caller", "203.0.113.1", 8090)
	reachability := &stubReachability{
		answered:    map[yagomodel.Hash]peerwire.Endpoint{caller.Hash: {Scheme: "http"}},
		answeredErr: errors.New("roster unavailable"),
	}
	probe := &stubProbe{reachable: false}

	resp, err := newEndpoint(t, probe, reachability).Serve(
		t.Context(), helloRequest("freeworld", caller, 0),
	)

	if err != nil || resp.YourType != yagomodel.PeerJunior {
		t.Fatalf("hello = %q/%v", resp.YourType, err)
	}
	if probe.answered.Present() || len(reachability.remembered) != 0 {
		t.Fatalf("probe answered = %+v, remembered = %v", probe.answered, reachability.remembered)
	}
}
