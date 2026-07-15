package yagonode

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/hosttrust"
)

type trustedDomainCatalogFixture struct {
	policy   hosttrust.Policy
	replaced hosttrust.Policy
	err      error
}

func (catalog *trustedDomainCatalogFixture) Current() hosttrust.Policy {
	return catalog.policy
}

func (catalog *trustedDomainCatalogFixture) Replace(
	_ context.Context,
	policy hosttrust.Policy,
) error {
	catalog.replaced = policy

	return catalog.err
}

func TestRankingConsoleHostTrustOperations(t *testing.T) {
	base := newRankingConsole(
		testRankingHolder(t),
		fakeRanker{},
		fakeCurated{},
	).(adminui.HostTrustSource)
	if got, available := base.HostTrust(t.Context()); available || got.Domains != nil {
		t.Fatalf("missing catalog view = %#v/%v", got, available)
	}
	if err := base.ApplyHostTrust(t.Context(), adminui.HostTrustView{}); err == nil {
		t.Fatal("missing catalog accepted a policy")
	}

	catalog := &trustedDomainCatalogFixture{policy: hosttrust.Policy{
		Blend: 0.35, Domains: []string{"a.example"},
	}}
	source := newRankingConsole(
		testRankingHolder(t),
		fakeRanker{},
		fakeCurated{},
		rankingConsoleLearning{trust: catalog},
	).(adminui.HostTrustSource)
	view, available := source.HostTrust(t.Context())
	if !available {
		t.Fatal("host trust policy is unavailable")
	}
	if !reflect.DeepEqual(view, adminui.HostTrustView{
		Blend: 0.35, Domains: []string{"a.example"},
	}) {
		t.Fatalf("trust view = %#v", view)
	}
	view.Domains[0] = "changed.example"
	if catalog.policy.Domains[0] != "a.example" {
		t.Fatal("trust view aliases the catalog policy")
	}

	input := adminui.HostTrustView{Blend: 0.5, Domains: []string{"b.example"}}
	if err := source.ApplyHostTrust(t.Context(), input); err != nil {
		t.Fatal(err)
	}
	input.Domains[0] = "changed.example"
	if !reflect.DeepEqual(catalog.replaced, hosttrust.Policy{
		Blend: 0.5, Domains: []string{"b.example"},
	}) {
		t.Fatalf("replacement = %#v", catalog.replaced)
	}

	catalog.err = errors.New("disk")
	if err := source.ApplyHostTrust(t.Context(), input); err == nil {
		t.Fatal("catalog failure did not surface")
	}
}
