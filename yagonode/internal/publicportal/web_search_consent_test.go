package publicportal

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
)

type webSearchConsentProbe struct {
	consented bool
}

func (p *webSearchConsentProbe) Search(
	context.Context,
	string,
	string,
	int,
	int,
) (SearchResults, error) {
	return SearchResults{}, nil
}

func (p *webSearchConsentProbe) SearchWithWebConsent(
	_ context.Context,
	_, _ string,
	_, _ int,
	consented bool,
) (SearchResults, error) {
	p.consented = consented

	return SearchResults{}, nil
}

func TestPortalWebSearchRequiresExplicitCheckboxConsent(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name      string
		query     string
		consented bool
		checked   bool
	}{
		{name: "absent", query: "q=sample"},
		{name: "invalid value", query: "q=sample&web=true"},
		{name: "checked", query: "q=sample&web=1", consented: true, checked: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			probe := &webSearchConsentProbe{}
			handler := New(probe, false)
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest("GET", "/?"+test.query, nil)
			handler.ServeHTTP(recorder, request)

			if recorder.Code != 200 || probe.consented != test.consented {
				t.Fatalf("status=%d consent=%v, want status 200 and consent %v",
					recorder.Code, probe.consented, test.consented)
			}
			checked := strings.Contains(recorder.Body.String(), `name="web" value="1" checked`)
			if checked != test.checked {
				t.Fatalf("checkbox checked=%v, want %v", checked, test.checked)
			}
		})
	}
}

func TestPortalPageURLsKeepExplicitWebConsent(t *testing.T) {
	t.Parallel()

	pages := numberedPages("sample", "", 1, 20, true, true)
	if len(pages) == 0 || !strings.Contains(pages[0].URL, "web=1") {
		t.Fatalf("numbered page URL did not retain web consent: %+v", pages)
	}
	if strings.Contains(portalPageURL("sample", "", 2, false), "web=1") {
		t.Fatal("unconsented page URL added web consent")
	}
}
