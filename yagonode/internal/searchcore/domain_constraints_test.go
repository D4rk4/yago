package searchcore

import "testing"

type resultDomainConstraintExpectation struct {
	name   string
	req    Request
	result Result
	want   bool
}

var resultDomainConstraintExpectations = []resultDomainConstraintExpectation{
	{
		name:   "unconstrained malformed URL",
		result: Result{URL: "://invalid"},
		want:   true,
	},
	{
		name:   "included parent domain",
		req:    Request{IncludeDomains: []string{"example.org"}},
		result: Result{URL: "https://docs.example.org/guide"},
		want:   true,
	},
	{
		name:   "included host field",
		req:    Request{IncludeDomains: []string{"example.org"}},
		result: Result{Host: "WWW.EXAMPLE.ORG.", URL: "://invalid"},
		want:   true,
	},
	{
		name:   "included fallback host field with port",
		req:    Request{IncludeDomains: []string{"example.org"}},
		result: Result{Host: "WWW.EXAMPLE.ORG.:8443", URL: "://invalid"},
		want:   true,
	},
	{
		name:   "included fallback IP address",
		req:    Request{IncludeDomains: []string{"192.0.2.10"}},
		result: Result{Host: "192.0.2.10", URL: "://invalid"},
		want:   true,
	},
	{
		name: "canonical URL host wins over mismatched field",
		req:  Request{IncludeDomains: []string{"allowed.example"}},
		result: Result{
			Host: "blocked.example", URL: "https://allowed.example:8443/result",
		},
		want: true,
	},
	{
		name: "canonical excluded URL host wins over allowed field",
		req:  Request{ExcludeDomains: []string{"blocked.example"}},
		result: Result{
			Host: "allowed.example", URL: "https://blocked.example:8443/result",
		},
		want: false,
	},
	{
		name:   "suffix confusion",
		req:    Request{IncludeDomains: []string{"example.org"}},
		result: Result{URL: "https://example.org.evil/guide"},
		want:   false,
	},
	{
		name: "excluded subdomain wins",
		req: Request{
			IncludeDomains: []string{"example.org"},
			ExcludeDomains: []string{"blocked.example.org"},
		},
		result: Result{URL: "https://deep.blocked.example.org/guide"},
		want:   false,
	},
	{
		name:   "exclude only leaves unknown host",
		req:    Request{ExcludeDomains: []string{"blocked.example"}},
		result: Result{URL: "://invalid"},
		want:   true,
	},
	{
		name:   "include requires a host",
		req:    Request{IncludeDomains: []string{"example.org"}},
		result: Result{URL: "://invalid"},
		want:   false,
	},
	{
		name:   "malformed fallback authority is not trusted",
		req:    Request{IncludeDomains: []string{"example.org"}},
		result: Result{Host: "example.org/path", URL: "://invalid"},
		want:   false,
	},
}

func TestResultSatisfiesDomainConstraints(t *testing.T) {
	for _, test := range resultDomainConstraintExpectations {
		t.Run(test.name, func(t *testing.T) {
			if got := ResultSatisfiesDomainConstraints(test.req, test.result); got != test.want {
				t.Fatalf("ResultSatisfiesDomainConstraints() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestResultSatisfiesBracketedIPv6DomainConstraint(t *testing.T) {
	result := Result{URL: "https://[2001:db8::1]/reference"}
	if !ResultSatisfiesDomainConstraints(
		Request{IncludeDomains: []string{"[2001:DB8::1]"}},
		result,
	) {
		t.Fatal("bracketed IPv6 constraint rejected matching URL")
	}
	if ResultSatisfiesDomainConstraints(
		Request{ExcludeDomains: []string{"[2001:DB8::1]"}},
		result,
	) {
		t.Fatal("bracketed IPv6 exclusion retained matching URL")
	}
}

func TestResponseSatisfyingDomainConstraintsKeepsHonestMaterializedTotal(t *testing.T) {
	response := responseSatisfyingDomainConstraints(
		Request{IncludeDomains: []string{"allowed.example"}},
		Response{
			TotalResults: 2,
			Availability: ResultAvailability{Materialized: 2, Exhausted: true},
			Results: []Result{
				{URL: "https://blocked.example/"},
				{URL: "https://allowed.example/"},
			},
		},
	)
	if response.TotalResults != 1 || response.Availability.Materialized != 1 ||
		!response.Availability.Exhausted || len(response.Results) != 1 ||
		response.Results[0].URL != "https://allowed.example/" {
		t.Fatalf("response = %#v", response)
	}
}

func TestResponseSatisfyingDomainConstraintsPreservesAcceptedResponse(t *testing.T) {
	response := Response{
		TotalResults: 1,
		Availability: ResultAvailability{Materialized: 1, Exhausted: true},
		Results:      []Result{{URL: "https://allowed.example/"}},
	}
	got := responseSatisfyingDomainConstraints(
		Request{IncludeDomains: []string{"allowed.example"}},
		response,
	)
	if got.TotalResults != response.TotalResults ||
		got.Availability != response.Availability ||
		len(got.Results) != len(response.Results) ||
		got.Results[0].URL != response.Results[0].URL {
		t.Fatalf("response = %#v", got)
	}
}
