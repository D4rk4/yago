package pagefetch_test

import (
	"errors"
	"fmt"
	"net/url"
	"testing"

	"github.com/D4rk4/yago/yago-crawler/internal/pagefetch"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestAsNotModifiedRecognizesOnlyNotModifiedOutcomes(t *testing.T) {
	validators := yagocrawlcontract.HTTPValidators{ETag: `"v2"`}
	wrapped := fmt.Errorf("fetch page: %w", &pagefetch.NotModifiedError{Validators: validators})

	notModified, ok := pagefetch.AsNotModified(wrapped)
	if !ok || notModified.Validators != validators || notModified.Error() == "" {
		t.Fatalf(
			"AsNotModified(%v) = %+v, %v, want the wrapped validators",
			wrapped,
			notModified,
			ok,
		)
	}
	for _, err := range []error{nil, pagefetch.ErrPageRejected, errors.New("connection reset")} {
		if notModified, ok := pagefetch.AsNotModified(err); ok || notModified != nil {
			t.Fatalf(
				"AsNotModified(%v) = %+v, %v, want no not-modified outcome",
				err,
				notModified,
				ok,
			)
		}
	}
}

func TestUnconditionalFetchRequestKeepsOnlyTheTarget(t *testing.T) {
	target := &url.URL{Scheme: "https", Host: "example.test", Path: "/page"}
	request := pagefetch.FetchRequest{
		Target: target,
		Validators: yagocrawlcontract.HTTPValidators{
			ETag:         `"v1"`,
			LastModified: "Wed, 01 Jul 2026 10:00:00 GMT",
		},
	}

	unconditional := request.Unconditional()
	if unconditional.Target != target ||
		unconditional.Validators != (yagocrawlcontract.HTTPValidators{}) {
		t.Fatalf("Unconditional() = %+v, want the target without validators", unconditional)
	}
	if request.Validators.ETag != `"v1"` {
		t.Fatalf("original request validators = %+v, want them unchanged", request.Validators)
	}
}
