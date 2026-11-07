package yagocrawlcontract

import (
	"errors"
	"testing"
)

func issuedConditionalFetch() *ConditionalFetch {
	return &ConditionalFetch{
		Validators: HTTPValidators{
			ETag:         `W/"v1"`,
			LastModified: "Sun, 06 Nov 1994 08:49:37 GMT",
		},
		Revision: storedDocumentRevision(),
	}
}

func TestCrawlRequestWithoutConditionalFetchIsUnconditional(t *testing.T) {
	for _, mode := range []CrawlRequestMode{
		"",
		CrawlRequestModeURL,
		CrawlRequestModeSitemap,
		CrawlRequestModeSitelist,
		CrawlRequestModeRobots,
	} {
		request := CrawlRequest{URL: "https://example.org/", Mode: mode}
		if err := request.ValidateConditionalFetch(); err != nil {
			t.Errorf("mode %q: ValidateConditionalFetch() = %v", mode, err)
		}
	}
}

func TestCrawlRequestAcceptsConditionalFetchOnPageRequest(t *testing.T) {
	for _, mode := range []CrawlRequestMode{"", CrawlRequestModeURL} {
		request := CrawlRequest{
			URL:              "https://example.org/",
			Mode:             mode,
			ConditionalFetch: issuedConditionalFetch(),
		}
		if err := request.ValidateConditionalFetch(); err != nil {
			t.Errorf("mode %q: ValidateConditionalFetch() = %v", mode, err)
		}
	}
	onlyEntityTag := issuedConditionalFetch()
	onlyEntityTag.Validators.LastModified = ""
	onlyDate := issuedConditionalFetch()
	onlyDate.Validators.ETag = ""
	for _, fetch := range []*ConditionalFetch{onlyEntityTag, onlyDate} {
		request := CrawlRequest{URL: "https://example.org/", ConditionalFetch: fetch}
		if err := request.ValidateConditionalFetch(); err != nil {
			t.Errorf("ValidateConditionalFetch(%+v) = %v", fetch.Validators, err)
		}
	}
}

func TestCrawlRequestRefusesConditionalFetchOnExpandedRequest(t *testing.T) {
	for _, mode := range []CrawlRequestMode{
		CrawlRequestModeSitemap,
		CrawlRequestModeSitelist,
		CrawlRequestModeRobots,
	} {
		request := CrawlRequest{
			URL:              "https://example.org/list",
			Mode:             mode,
			ConditionalFetch: issuedConditionalFetch(),
		}
		if err := request.ValidateConditionalFetch(); !errors.Is(err, errConditionalFetchMode) {
			t.Errorf("mode %q: ValidateConditionalFetch() = %v, want %v",
				mode, err, errConditionalFetchMode)
		}
	}
}

func TestCrawlRequestRefusesInvalidConditionalFetch(t *testing.T) {
	invalidEntityTag := issuedConditionalFetch()
	invalidEntityTag.Validators.ETag = "\"v1\"\r\nX-Injected: 1"
	invalidDate := issuedConditionalFetch()
	invalidDate.Validators.LastModified = "06 Nov 1994"
	absentDocument := issuedConditionalFetch()
	absentDocument.Revision = DocumentRevision{}
	cases := []struct {
		name  string
		fetch *ConditionalFetch
		want  error
	}{
		{"empty", &ConditionalFetch{}, errHTTPValidatorsEmpty},
		{
			"no validators",
			&ConditionalFetch{Revision: storedDocumentRevision()},
			errHTTPValidatorsEmpty,
		},
		{"invalid entity tag", invalidEntityTag, errEntityTagSyntax},
		{"invalid date", invalidDate, errHTTPDateSyntax},
		{"absent stored document", absentDocument, errDocumentRevisionIncomplete},
	}
	for _, test := range cases {
		request := CrawlRequest{URL: "https://example.org/", ConditionalFetch: test.fetch}
		if err := request.ValidateConditionalFetch(); !errors.Is(err, test.want) {
			t.Errorf("%s: ValidateConditionalFetch() = %v, want %v", test.name, err, test.want)
		}
	}
}
