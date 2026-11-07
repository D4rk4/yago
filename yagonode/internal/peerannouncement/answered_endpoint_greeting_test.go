package peerannouncement

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

func (s *stubRoster) AnsweredEndpoint(
	_ context.Context,
	peer yagomodel.Hash,
) (peerwire.Endpoint, bool, error) {
	endpoint, found := s.answered[peer]

	return endpoint, found, s.answeredErr
}

type helloAnswer func(*url.URL) (*http.Response, error)

func helloAnswerFrom(t *testing.T, responder yagomodel.Seed) *http.Response {
	t.Helper()
	body := yagoproto.HelloResponse{
		YourIP:   "203.0.113.9",
		YourType: yagomodel.PeerSenior,
		Seeds:    []yagomodel.Seed{responder},
	}.Encode().Encode()

	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}
}

func recordingHelloClient(answer helloAnswer) (*http.Client, *[]string) {
	attempts := []string{}
	client := &http.Client{
		Timeout: time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			attempts = append(attempts, peerwire.EndpointAt(request.URL).Origin())

			return answer(request.URL)
		}),
	}

	return client, &attempts
}

func answerOnlyAt(t *testing.T, responder yagomodel.Seed, origin string) helloAnswer {
	t.Helper()

	return func(target *url.URL) (*http.Response, error) {
		if peerwire.EndpointAt(target).Origin() != origin {
			return nil, errors.New("endpoint unavailable")
		}

		return helloAnswerFrom(t, responder), nil
	}
}

func endpointAt(t *testing.T, origin string) peerwire.Endpoint {
	t.Helper()
	endpoint, err := peerwire.ParseEndpointOrigin(origin)
	if err != nil {
		t.Fatal(err)
	}

	return endpoint
}

func dualStackTarget(t *testing.T) yagomodel.Seed {
	t.Helper()
	target := callerSeed(t, "target", "192.0.2.7")
	alternatives, err := yagomodel.ParseIP6("2001:db8::7")
	if err != nil {
		t.Fatal(err)
	}
	target.IP6 = yagomodel.Some(alternatives)

	return target
}

func greetWith(
	t *testing.T,
	client *http.Client,
	preferHTTPS bool,
	target yagomodel.Seed,
	answered yagomodel.Optional[peerwire.Endpoint],
) (greetResult, error) {
	t.Helper()

	return newHTTPPeerGreeter(client, "freeworld", preferHTTPS).Greet(
		t.Context(), target, callerSeed(t, "self", "203.0.113.9"), 0, answered,
	)
}

func assertAttempts(t *testing.T, attempts *[]string, want ...string) {
	t.Helper()
	if !slices.Equal(*attempts, want) {
		t.Fatalf("attempts = %v, want %v", *attempts, want)
	}
}

func TestGreetTriesAnsweredEndpointFirstAndReportsExactContact(t *testing.T) {
	target := dualStackTarget(t)
	answered := endpointAt(t, "http://[2001:db8::7]:8090")
	client, attempts := recordingHelloClient(func(*url.URL) (*http.Response, error) {
		return helloAnswerFrom(t, target), nil
	})

	result, err := greetWith(t, client, false, target, yagomodel.Some(answered))

	if err != nil || result.ContactedEndpoint != answered {
		t.Fatalf("greet = %+v/%v, want contact at %+v", result.ContactedEndpoint, err, answered)
	}
	assertAttempts(t, attempts, "http://[2001:db8::7]:8090")

	client, attempts = recordingHelloClient(func(*url.URL) (*http.Response, error) {
		return helloAnswerFrom(t, target), nil
	})
	result, err = greetWith(t, client, false, target, yagomodel.None[peerwire.Endpoint]())
	if err != nil || result.ContactedEndpoint != endpointAt(t, "http://192.0.2.7:8090") {
		t.Fatalf("advertised greet = %+v/%v", result.ContactedEndpoint, err)
	}
	assertAttempts(t, attempts, "http://192.0.2.7:8090")
}

func TestGreetAddsImplicitHTTPSOnlyForDNSNamesUnderPreference(t *testing.T) {
	named := callerSeed(t, "target", "example.org")
	unavailable := func(*url.URL) (*http.Response, error) {
		return nil, errors.New("endpoint unavailable")
	}
	cases := []struct {
		target      yagomodel.Seed
		preferHTTPS bool
		want        []string
	}{
		{named, true, []string{"https://example.org:443", "http://example.org:8090"}},
		{named, false, []string{"http://example.org:8090"}},
		{dualStackTarget(t), true, []string{"http://192.0.2.7:8090", "http://[2001:db8::7]:8090"}},
	}
	for _, tc := range cases {
		client, attempts := recordingHelloClient(unavailable)
		result, err := greetWith(
			t,
			client,
			tc.preferHTTPS,
			tc.target,
			yagomodel.None[peerwire.Endpoint](),
		)
		if err == nil || result.ContactedEndpoint != (peerwire.Endpoint{}) {
			t.Fatalf("failed greet = %+v/%v", result.ContactedEndpoint, err)
		}
		assertAttempts(t, attempts, tc.want...)
	}

	client, attempts := recordingHelloClient(answerOnlyAt(t, named, "https://example.org:443"))
	result, err := greetWith(t, client, true, named, yagomodel.None[peerwire.Endpoint]())
	if err != nil || result.ContactedEndpoint != endpointAt(t, "https://example.org:443") {
		t.Fatalf("implicit HTTPS greet = %+v/%v", result.ContactedEndpoint, err)
	}
	assertAttempts(t, attempts, "https://example.org:443")
}

func TestGreetRecordsOnlyTheEndpointWhoseAnswerMatchesTarget(t *testing.T) {
	target := dualStackTarget(t)
	impostor := callerSeed(t, "impostor", "192.0.2.7")
	for name, primaryAnswer := range map[string]helloAnswer{
		"wrong identity": func(*url.URL) (*http.Response, error) {
			return helloAnswerFrom(t, impostor), nil
		},
		"malformed": func(*url.URL) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("not a hello response")),
			}, nil
		},
		"redirect": func(*url.URL) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusFound,
				Header:     http.Header{"Location": {"http://198.51.100.9:8090/yacy/hello.html"}},
				Body:       io.NopCloser(strings.NewReader("")),
			}, nil
		},
		"failed request": func(*url.URL) (*http.Response, error) {
			return nil, errors.New("connection refused")
		},
	} {
		client, attempts := recordingHelloClient(func(endpoint *url.URL) (*http.Response, error) {
			if endpoint.Hostname() == "192.0.2.7" {
				return primaryAnswer(endpoint)
			}

			return helloAnswerFrom(t, target), nil
		})

		result, err := greetWith(t, client, false, target, yagomodel.None[peerwire.Endpoint]())

		if err != nil || result.ContactedEndpoint != endpointAt(t, "http://[2001:db8::7]:8090") {
			t.Fatalf("%s: greet = %+v/%v", name, result.ContactedEndpoint, err)
		}
		assertAttempts(t, attempts, "http://192.0.2.7:8090", "http://[2001:db8::7]:8090")
	}
}

func TestGreetWithExpiredOperationAttemptsNothing(t *testing.T) {
	target := dualStackTarget(t)
	client, attempts := recordingHelloClient(func(*url.URL) (*http.Response, error) {
		return helloAnswerFrom(t, target), nil
	})
	canceled, cancel := context.WithCancel(t.Context())
	cancel()

	result, err := newHTTPPeerGreeter(client, "freeworld", false).Greet(
		canceled,
		target,
		callerSeed(t, "self", "203.0.113.9"),
		0,
		yagomodel.Some(endpointAt(t, "http://[2001:db8::7]:8090")),
	)

	if !errors.Is(err, context.Canceled) || result.ContactedEndpoint != (peerwire.Endpoint{}) {
		t.Fatalf("canceled greet = %+v/%v", result.ContactedEndpoint, err)
	}
	assertAttempts(t, attempts)
}

func TestAnnounceGreetsAtAnsweredEndpointAndRemembersContact(t *testing.T) {
	peer := callerSeed(t, "peer", "203.0.113.1")
	remembered := endpointAt(t, "http://203.0.113.1:8090")
	contacted := endpointAt(t, "https://203.0.113.1:8443")
	roster := &stubRoster{
		rounds:   [][]yagomodel.Seed{{peer}},
		answered: map[yagomodel.Hash]peerwire.Endpoint{peer.Hash: remembered},
	}
	greeter := &stubGreeter{result: greetResult{
		YourType:          yagomodel.PeerSenior,
		ContactedEndpoint: contacted,
	}}
	a := &announcer{
		self:    stubSelf{seed: callerSeed(t, "self", "203.0.113.9")},
		seeds:   &stubSeedSource{},
		roster:  roster,
		greeter: greeter,
	}

	a.Announce(t.Context())

	if len(greeter.answered) != 1 || greeter.answered[0] != yagomodel.Some(remembered) {
		t.Fatalf("greeter answered endpoints = %v", greeter.answered)
	}
	if len(roster.responders) != 1 || roster.remembered[peer.Hash] != contacted {
		t.Fatalf("responders = %v, remembered = %v", roster.responders, roster.remembered)
	}
}

func TestAnnounceGreetsAdvertisedEndpointsWithoutUsableMemory(t *testing.T) {
	peer := callerSeed(t, "peer", "203.0.113.1")
	for name, roster := range map[string]*stubRoster{
		"absent": {rounds: [][]yagomodel.Seed{{peer}}},
		"unreadable": {
			rounds:      [][]yagomodel.Seed{{peer}},
			answered:    map[yagomodel.Hash]peerwire.Endpoint{peer.Hash: {Scheme: "http"}},
			answeredErr: errors.New("roster unavailable"),
		},
	} {
		greeter := &stubGreeter{result: greetResult{YourType: yagomodel.PeerSenior}}
		a := &announcer{
			self:    stubSelf{seed: callerSeed(t, "self", "203.0.113.9")},
			seeds:   &stubSeedSource{},
			roster:  roster,
			greeter: greeter,
		}

		a.Announce(t.Context())

		if len(greeter.answered) != 1 || greeter.answered[0].Present() {
			t.Fatalf("%s: greeter answered endpoints = %v", name, greeter.answered)
		}
	}
}

func TestFailedGreetRemembersNoEndpoint(t *testing.T) {
	peer := callerSeed(t, "peer", "203.0.113.1")
	roster := &stubRoster{rounds: [][]yagomodel.Seed{{peer}}}
	a := &announcer{
		self:    stubSelf{seed: callerSeed(t, "self", "203.0.113.9")},
		seeds:   &stubSeedSource{},
		roster:  roster,
		greeter: &stubGreeter{err: errGreetFailed},
	}

	a.Announce(t.Context())
	a.GreetDiscovered(t.Context(), peer)

	if len(roster.remembered) != 0 || len(roster.responders) != 0 {
		t.Fatalf("failed greet remembered %v, observed %v", roster.remembered, roster.responders)
	}
}

func TestGreetDiscoveredRemembersVerifiedContact(t *testing.T) {
	peer := callerSeed(t, "lan", "192.0.2.40")
	contacted := endpointAt(t, "http://192.0.2.40:8090")
	roster := &stubRoster{}
	a := &announcer{
		self:   stubSelf{seed: callerSeed(t, "self", "203.0.113.9")},
		seeds:  &stubSeedSource{},
		roster: roster,
		greeter: &stubGreeter{result: greetResult{
			YourType:          yagomodel.PeerSenior,
			ContactedEndpoint: contacted,
		}},
	}

	a.GreetDiscovered(t.Context(), peer)

	if roster.remembered[peer.Hash] != contacted {
		t.Fatalf("remembered = %v, want %+v", roster.remembered, contacted)
	}
}
