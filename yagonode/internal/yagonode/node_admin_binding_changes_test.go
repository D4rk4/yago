package yagonode

import (
	"context"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/events"
)

func TestBindingSourcePersistsOptionalDisabledStatesAndReenablesThem(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name            string
		key             string
		environmentPort string
		reenabledPort   string
	}{
		{name: "public search", key: bindKeyPublic, environmentPort: "8080", reenabledPort: "8081"},
		{name: "crawler exchange", key: bindKeyCrawler, environmentPort: "9091", reenabledPort: "9092"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assertOptionalBindingDisableAndReenable(
				t,
				tc.key,
				tc.environmentPort,
				tc.reenabledPort,
			)
		})
	}
}

func assertOptionalBindingDisableAndReenable(
	t *testing.T,
	key string,
	environmentPort string,
	reenabledPort string,
) {
	t.Helper()

	source, store, _ := newTestBindingSource(t, nodeConfig{
		PeerAddr: ":8090", PublicAddr: ":8080", OpsAddr: ":9090",
		Crawl: crawlConfig{ListenAddr: ":9091"},
	})
	ctx := context.Background()

	result, err := source.UpdateBinding(ctx, adminui.BindChange{
		Key: key, Action: adminui.BindActionDisable,
	})
	if err != nil || !result.OK || !strings.Contains(result.Message, "will be disabled") {
		t.Fatalf("disable = %+v err=%v", result, err)
	}
	stored, set, err := store.Get(ctx, key)
	if err != nil || !set || stored != disabledBindOverride {
		t.Fatalf("stored disabled bind = (%q,%v) err=%v", stored, set, err)
	}

	disabled := bindingItemByKey(t, source.Bindings(ctx), key)
	if disabled.ListenerEnabled || !disabled.CanDisable || !disabled.Overridden ||
		disabled.Host != "" || disabled.Port != environmentPort {
		t.Fatalf("disabled item = %+v", disabled)
	}

	result, err = source.UpdateBinding(ctx, adminui.BindChange{
		Key: key, Host: "127.0.0.1", Port: reenabledPort,
		Action: adminui.BindActionSet,
	})
	if err != nil || !result.OK {
		t.Fatalf("reenable = %+v err=%v", result, err)
	}
	stored, set, err = store.Get(ctx, key)
	wantStored := "127.0.0.1:" + reenabledPort
	if err != nil || !set || stored != wantStored {
		t.Fatalf("stored enabled bind = (%q,%v) err=%v", stored, set, err)
	}

	enabled := bindingItemByKey(t, source.Bindings(ctx), key)
	if !enabled.ListenerEnabled || !enabled.CanDisable || !enabled.Overridden ||
		enabled.Host != "127.0.0.1" || enabled.Port != reenabledPort {
		t.Fatalf("reenabled item = %+v", enabled)
	}
}

func TestBindingSourceResetRestoresEnvironmentAuthorityForEveryListener(t *testing.T) {
	t.Parallel()

	source, store, recorder := newTestBindingSource(t, nodeConfig{
		PeerAddr: ":8090", PublicAddr: "127.0.0.1:8080", OpsAddr: ":9090",
		Crawl: crawlConfig{ListenAddr: "127.0.0.1:9091"},
	})
	ctx := context.Background()

	for _, key := range []string{bindKeyPeer, bindKeyPublic, bindKeyCrawler, bindKeyOps} {
		if err := store.Set(ctx, key, "127.0.0.1:9999"); err != nil {
			t.Fatalf("seed %s: %v", key, err)
		}
		result, err := source.UpdateBinding(ctx, adminui.BindChange{
			Key: key, Action: adminui.BindActionReset,
		})
		if err != nil || !result.OK ||
			!strings.Contains(result.Message, "environment listen address") {
			t.Fatalf("reset %s = %+v err=%v", key, result, err)
		}
		if _, set, err := store.Get(ctx, key); err != nil || set {
			t.Fatalf("reset %s retained override: set=%v err=%v", key, set, err)
		}
	}

	view := source.Bindings(ctx)
	if got := bindingItemByKey(t, view, bindKeyPeer).Port; got != "8090" {
		t.Fatalf("peer environment port = %q", got)
	}
	if got := bindingItemByKey(t, view, bindKeyPublic).Port; got != "8080" {
		t.Fatalf("public environment port = %q", got)
	}
	if got := bindingItemByKey(t, view, bindKeyCrawler).Port; got != "9091" {
		t.Fatalf("crawler environment port = %q", got)
	}
	if got := bindingItemByKey(t, view, bindKeyOps).Port; got != "9090" {
		t.Fatalf("ops environment port = %q", got)
	}
	if recent := recorder.Recent(4); len(recent) != 4 {
		t.Fatalf("reset events = %+v", recent)
	} else {
		for _, event := range recent {
			if event.Category != events.CategoryConfig ||
				!strings.Contains(event.Message, "environment") {
				t.Fatalf("reset event = %+v", event)
			}
		}
	}
}

func TestBindingSourceRejectsInvalidActionsAndRequiredListenerDisable(t *testing.T) {
	t.Parallel()

	source, store, _ := newTestBindingSource(t, nodeConfig{})
	ctx := context.Background()

	for _, tc := range []struct {
		change  adminui.BindChange
		message string
	}{
		{
			change:  adminui.BindChange{Key: bindKeyPeer, Action: adminui.BindActionDisable},
			message: bindingRequiredListenerDisableMessage,
		},
		{
			change: adminui.BindChange{
				Key: bindKeyPublic, Action: adminui.BindAction("future-action"),
			},
			message: bindingUnknownActionMessage,
		},
	} {
		result, err := source.UpdateBinding(ctx, tc.change)
		if err != nil || result.OK || result.Message != tc.message {
			t.Fatalf("change %+v = %+v err=%v", tc.change, result, err)
		}
	}
	if overrides, err := store.All(ctx); err != nil || len(overrides) != 0 {
		t.Fatalf("rejected actions stored overrides: %+v err=%v", overrides, err)
	}
}

func TestBindingSourceRejectsDisabledOverrideForRequiredListener(t *testing.T) {
	t.Parallel()

	source, store, _ := newTestBindingSource(t, nodeConfig{})
	if err := store.Set(context.Background(), bindKeyPeer, disabledBindOverride); err != nil {
		t.Fatalf("Set: %v", err)
	}
	view := source.Bindings(context.Background())
	if view.Error != storedBindingsUnavailable || len(view.Items) != 0 {
		t.Fatalf("invalid required-listener override = %+v", view)
	}
}

func TestBindingSourceRejectsInvalidEnvironmentSuggestionBehindDisabledOverride(t *testing.T) {
	t.Parallel()

	source, store, _ := newTestBindingSource(t, nodeConfig{
		PeerAddr: ":8090", PublicAddr: "invalid", OpsAddr: ":9090",
	})
	if err := store.Set(context.Background(), bindKeyPublic, disabledBindOverride); err != nil {
		t.Fatalf("Set: %v", err)
	}
	view := source.Bindings(context.Background())
	if view.Error != storedBindingsUnavailable || len(view.Items) != 0 {
		t.Fatalf("invalid environment suggestion = %+v", view)
	}
}

func bindingItemByKey(
	t *testing.T,
	view adminui.BindingsView,
	key string,
) adminui.BindItem {
	t.Helper()

	for _, item := range view.Items {
		if item.Key == key {
			return item
		}
	}
	t.Fatalf("binding %q not present", key)

	return adminui.BindItem{}
}
