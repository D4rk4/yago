package remotecrawl

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"
)

const unresolvableAllowlistedDomain = "default-resolver.example.test"

func TestDestinationPolicyUsesTheDefaultResolver(t *testing.T) {
	if _, err := newDestinationPolicy([]string{" "}, publicResolver); err == nil {
		t.Fatal("empty policy accepted")
	}
	nameserverDials := replaceDefaultResolverWithUnreachableNameserver(t)
	destination := "https://" + unresolvableAllowlistedDomain + "/a"

	injectedResolverPolicy, err := newDestinationPolicy(
		[]string{unresolvableAllowlistedDomain},
		publicResolver,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := injectedResolverPolicy.Admit(t.Context(), destination); err != nil {
		t.Fatal(err)
	}
	if dials := nameserverDials.Load(); dials != 0 {
		t.Fatalf("injected resolver policy dialed the default nameserver %d times", dials)
	}

	defaultResolverPolicy, err := newDestinationPolicy(
		[]string{unresolvableAllowlistedDomain},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := defaultResolverPolicy.Admit(t.Context(), destination); err == nil {
		t.Fatal("destination admitted without a default resolver answer")
	}
	if nameserverDials.Load() == 0 {
		t.Fatal("default resolver was not consulted")
	}
}

func replaceDefaultResolverWithUnreachableNameserver(t *testing.T) *atomic.Int32 {
	t.Helper()
	var nameserverDials atomic.Int32
	previous := net.DefaultResolver
	net.DefaultResolver = &net.Resolver{
		PreferGo: true,
		Dial: func(context.Context, string, string) (net.Conn, error) {
			nameserverDials.Add(1)

			return nil, errors.New("nameserver unreachable")
		},
	}
	t.Cleanup(func() { net.DefaultResolver = previous })

	return &nameserverDials
}
