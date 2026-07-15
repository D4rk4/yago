package adminui

import (
	"context"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

type fakeHostTrustRanking struct {
	*fakeRanking
	trust      HostTrustView
	applied    HostTrustView
	applyCalls int
	applyErr   error
}

type unavailableHostTrustRanking struct {
	*fakeRanking
}

func (unavailableHostTrustRanking) HostTrust(context.Context) (HostTrustView, bool) {
	return HostTrustView{}, false
}

func (unavailableHostTrustRanking) ApplyHostTrust(
	context.Context,
	HostTrustView,
) error {
	return context.Canceled
}

func (ranking *fakeHostTrustRanking) HostTrust(context.Context) (HostTrustView, bool) {
	return ranking.trust, true
}

func (ranking *fakeHostTrustRanking) ApplyHostTrust(
	_ context.Context,
	trust HostTrustView,
) error {
	ranking.applyCalls++
	ranking.applied = trust
	if ranking.applyErr == nil {
		ranking.trust = trust
	}

	return ranking.applyErr
}

func TestConsoleYagoRankRendersHostTrust(t *testing.T) {
	t.Parallel()

	ranking := &fakeHostTrustRanking{
		fakeRanking: &fakeRanking{profile: sampleRankingProfile()},
		trust: HostTrustView{
			Blend: 0.35, Domains: []string{"a.example", "b.example"},
		},
	}
	body := do(t, New(Options{Ranking: ranking}), "/admin/yagorank").body
	for _, want := range []string{
		"Host trust",
		`name="trust_blend" value="0.35"`,
		`name="trust_domains"`,
		"a.example\nb.example",
		`value="save-trust"`,
		"2 trusted domains",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("host trust page missing %q", want)
		}
	}
}

func TestConsoleYagoRankHidesUnavailableHostTrust(t *testing.T) {
	t.Parallel()

	ranking := unavailableHostTrustRanking{
		fakeRanking: &fakeRanking{profile: sampleRankingProfile()},
	}
	body := do(t, New(Options{Ranking: ranking}), "/admin/yagorank").body
	if strings.Contains(body, "Host trust") || strings.Contains(body, "trusted domain") {
		t.Fatalf("unavailable host trust rendered as known: %s", body)
	}
}

func TestConsoleYagoRankSavesHostTrust(t *testing.T) {
	t.Parallel()

	ranking := &fakeHostTrustRanking{
		fakeRanking: &fakeRanking{profile: sampleRankingProfile()},
	}
	body := doPost(t, New(Options{Ranking: ranking}), "/admin/yagorank", url.Values{
		"action":        {"save-trust"},
		"trust_blend":   {"0.4"},
		"trust_domains": {"b.example\n a.example"},
	}).body
	if ranking.applyCalls != 1 || !reflect.DeepEqual(ranking.applied, HostTrustView{
		Blend: 0.4, Domains: []string{"b.example", "a.example"},
	}) {
		t.Fatalf("applied trust = %#v calls=%d", ranking.applied, ranking.applyCalls)
	}
	if !strings.Contains(body, "Host trust policy saved.") ||
		!strings.Contains(body, "2 trusted domains") {
		t.Fatalf("save result missing: %s", body)
	}
}

func TestConsoleYagoRankRejectsInvalidHostTrustBlend(t *testing.T) {
	t.Parallel()

	cases := []struct {
		blend string
		want  string
	}{
		{blend: "invalid", want: "Enter a number for trust blend."},
		{blend: "2", want: "Trust blend must be between zero and one."},
	}
	for _, test := range cases {
		t.Run(test.blend, func(t *testing.T) {
			ranking := &fakeHostTrustRanking{
				fakeRanking: &fakeRanking{profile: sampleRankingProfile()},
			}
			body := doPost(t, New(Options{Ranking: ranking}), "/admin/yagorank", url.Values{
				"action":        {"save-trust"},
				"trust_blend":   {test.blend},
				"trust_domains": {"a.example"},
			}).body
			if ranking.applyCalls != 0 || !strings.Contains(body, test.want) ||
				!strings.Contains(body, `value="`+test.blend+`"`) ||
				!strings.Contains(body, "1 trusted domain") {
				t.Fatalf("invalid blend result calls=%d body=%s", ranking.applyCalls, body)
			}
		})
	}
}

func TestConsoleYagoRankSurfacesHostTrustFailureAndMissingCapability(t *testing.T) {
	t.Parallel()

	ranking := &fakeHostTrustRanking{
		fakeRanking: &fakeRanking{profile: sampleRankingProfile()},
		applyErr:    context.DeadlineExceeded,
	}
	body := doPost(t, New(Options{Ranking: ranking}), "/admin/yagorank", url.Values{
		"action":        {"save-trust"},
		"trust_blend":   {"0.2"},
		"trust_domains": {"a.example"},
	}).body
	if !strings.Contains(body, "Save host trust failed: context deadline exceeded") ||
		!strings.Contains(body, "a.example") {
		t.Fatalf("apply failure missing: %s", body)
	}

	missing := doPost(
		t,
		New(Options{Ranking: &fakeRanking{profile: sampleRankingProfile()}}),
		"/admin/yagorank",
		url.Values{"action": {"save-trust"}},
	).body
	if !strings.Contains(missing, "Host trust settings are not available.") {
		t.Fatalf("missing capability result: %s", missing)
	}
}
