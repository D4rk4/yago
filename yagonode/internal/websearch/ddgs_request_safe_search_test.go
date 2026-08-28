package websearch

import (
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

const safeSearchDDGFixture = `<!doctype html><html><body>
<div class="result"><a class="result__a" href="https://www.rfc-editor.org/rfc/rfc9562.html">RFC 9562</a>
<span class="result__snippet">UUID specification</span></div>
</body></html>`

func TestRequestSafeSearchOverridesProviderModeAndSurvivesFiltering(t *testing.T) {
	var mutex sync.Mutex
	var modes []string
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (
		*http.Response,
		error,
	) {
		mutex.Lock()
		modes = append(modes, request.URL.Query().Get("kp"))
		mutex.Unlock()

		return htmlResponse(http.StatusOK, safeSearchDDGFixture), nil
	})}
	provider := NewDDGSProvider(DDGSConfig{
		Client: client, Backend: backendDuckDuckGo, SafeSearch: "off",
		CacheTTL: time.Minute, Now: fixedClock(),
	})
	primary := &stubSearcher{resp: searchcore.Response{
		Availability: searchcore.ResultAvailability{Exhausted: true},
		PartialFailures: []searchcore.PartialFailure{{
			Source: searchcore.PartialFailureSourceLocalSearch,
			Reason: "local source did not complete",
		}},
	}}
	searcher := searchcore.NewSafeSearchSearcher(
		NewParallelSearcher(primary, provider, enabled),
	)
	request := searchcore.Request{
		Query: "RFC 9562", Limit: 5, Verify: searchcore.VerifyFalse,
	}

	unsafe, err := searcher.Search(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	request.SafeSearch = true
	safe, err := searcher.Search(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if len(unsafe.Results) != 1 ||
		unsafe.Results[0].SafetyRating != searchcore.SafetyUnknown {
		t.Fatalf("unsafe results = %#v", unsafe.Results)
	}
	if len(safe.Results) != 1 ||
		safe.Results[0].SafetyRating != searchcore.SafetyProviderFiltered ||
		len(safe.PartialFailures) != 1 {
		t.Fatalf("safe response = %#v", safe)
	}
	mutex.Lock()
	defer mutex.Unlock()
	if len(modes) != 2 || modes[0] != "-2" || modes[1] != "1" {
		t.Fatalf("safe-search modes = %q, want [-2 1]", modes)
	}
}

func TestStrictSafeSearchRefusesBackendWithoutEnforcement(t *testing.T) {
	provider := NewDDGSProvider(DDGSConfig{
		Client: &http.Client{Transport: roundTripFunc(func(*http.Request) (
			*http.Response,
			error,
		) {
			t.Fatal("unsupported backend received a strict-safe request")

			return nil, nil
		})},
		Backend: backendBing, SafeSearch: safeSearchStrict, Now: fixedClock(),
	})

	if _, err := provider.Search(t.Context(), "RFC 9562", 5); !errors.Is(
		err,
		errStrictSafeSearchUnavailable,
	) {
		t.Fatalf("strict-safe error = %v", err)
	}
}
