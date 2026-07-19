package yagonode

import (
	"context"
	"net/http"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/metrics"
	"github.com/D4rk4/yago/yagonode/internal/settingsstore"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestRunBuildsEgressClientFromPersistedSettings(t *testing.T) {
	restoreMainSeams(t)
	setValidRunEnv(t)
	t.Setenv(envEgressAllowLAN, "false")
	t.Setenv(envEgressAllowCIDRs, "")

	engine := newCtrlEngine()
	persisted := ctrlVault(t, engine)
	settings, err := settingsstore.Open(persisted)
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]string{
		"security.egress.allow_private": "true",
		"security.egress.allow_cidrs":   "10.77.0.0/16,fd77::/48",
	} {
		if err := settings.Set(t.Context(), key, value); err != nil {
			t.Fatalf("persist %s: %v", key, err)
		}
	}

	runtimeStorage := ctrlVault(t, engine)
	openRuntimeVault = func(string, int64) (*vault.Vault, error) { return runtimeStorage, nil }
	builtClient := &http.Client{}
	built := false
	buildRuntimeEgressClient = func(config nodeConfig) *http.Client {
		built = true
		if !config.EgressAllowLAN {
			t.Error("persisted private-network override was not applied before client construction")
		}
		if got := formatPrefixes(config.EgressAllowedCIDRs); got != "10.77.0.0/16,fd77::/48" {
			t.Errorf("persisted CIDR override = %q", got)
		}

		return builtClient
	}
	assembleRuntimeNode = func(
		_ context.Context,
		_ nodeConfig,
		_ *vault.Vault,
		client *http.Client,
		_ nodeTelemetry,
	) (node, error) {
		if client != builtClient {
			t.Errorf("assembled client = %p, want %p", client, builtClient)
		}

		return node{
			announcer:     fakeAnnouncer{},
			sweeper:       &scriptedSweeper{},
			searchExplain: newSearchExplainEndpoint(nil, nil, nil, nil, nil),
		}, nil
	}
	serveRuntimeNode = func(
		context.Context,
		node,
		*metrics.EvictionMetrics,
		...namedServer,
	) error {
		return nil
	}

	if err := run(); err != nil {
		t.Fatal(err)
	}
	if !built {
		t.Fatal("runtime egress client was not built")
	}
}
