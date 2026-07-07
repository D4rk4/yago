package adminui

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
)

// settingsGate reports whether the calling page may edit key. A nil gate permits
// every key (the settings source rejects any key it does not own). A non-nil gate
// that returns false makes the batch respond 404 before anything is written, so a
// foreign key cannot be applied through a page's whitelist.
type settingsGate func(key string) bool

// applySettingsBatch parses a one-form settings submission and applies it,
// returning the aggregated outcome for the page's notice mechanism. ok is false
// when a submitted key fails gate, signalling the caller to respond 404 before
// any change is written.
//
// A non-empty "reset" field takes precedence over a save: it clears that single
// key's override, preserving the per-row reset behaviour. Otherwise every
// submitted "key" whose "value:<key>" differs from the node's current value is
// updated in one pass.
func (c *Console) applySettingsBatch(
	r *http.Request,
	gate settingsGate,
) (notice, errMsg string, ok bool) {
	if key := strings.TrimSpace(r.PostFormValue("reset")); key != "" {
		return c.resetSetting(r, gate, key)
	}

	return c.saveSettings(r, gate)
}

// resetSetting clears a single key's override, gated so a foreign key 404s.
func (c *Console) resetSetting(
	r *http.Request,
	gate settingsGate,
	key string,
) (notice, errMsg string, ok bool) {
	if gate != nil && !gate(key) {
		return "", "", false
	}
	result, err := c.settings.Update(r.Context(), SettingsChange{Key: key, Reset: true})
	if err != nil {
		slog.WarnContext(r.Context(), "admin settings reset failed", slog.Any("error", err))
	}
	notice, errMsg = settingsOutcome(result, err)

	return notice, errMsg, true
}

// saveSettings applies every changed row from a settings form in one pass. It
// gates each submitted key first so a single foreign key 404s the whole batch
// before anything is written.
func (c *Console) saveSettings(
	r *http.Request,
	gate settingsGate,
) (notice, errMsg string, ok bool) {
	keys := r.PostForm["key"]
	for _, key := range keys {
		if gate != nil && !gate(key) {
			return "", "", false
		}
	}
	result, err := c.applyChangedSettings(r.Context(), r, keys)
	notice, errMsg = settingsOutcome(result, err)

	return notice, errMsg, true
}

// applyChangedSettings updates every submitted key whose value differs from the
// node's current value, aggregating the per-key outcomes into one result so an
// unchanged Save writes nothing and never marks a setting overridden. It stops at
// the first rejection or error so the page reports that reason.
func (c *Console) applyChangedSettings(
	ctx context.Context,
	r *http.Request,
	keys []string,
) (SettingsResult, error) {
	current := currentSettingValues(ctx, c.settings)
	applied := 0
	restart := false
	for _, key := range keys {
		value := submittedSettingValue(r, key)
		if value == current[key] {
			continue
		}
		result, err := c.settings.Update(ctx, SettingsChange{Key: key, Value: value})
		if err != nil {
			slog.WarnContext(ctx, "admin settings update failed", slog.Any("error", err))

			return SettingsResult{}, fmt.Errorf("apply runtime setting: %w", err)
		}
		if !result.OK {
			return result, nil
		}
		applied++
		restart = restart || result.RestartRequired
	}

	return SettingsResult{OK: true, RestartRequired: restart, Message: batchMessage(applied)}, nil
}

// batchMessage summarises how many settings a save applied.
func batchMessage(applied int) string {
	switch applied {
	case 0:
		return "No changes."
	case 1:
		return "1 setting updated."
	default:
		return fmt.Sprintf("%d settings updated.", applied)
	}
}

// currentSettingValues snapshots the node's current setting values by key so a
// batch save can detect which rows actually changed.
func currentSettingValues(ctx context.Context, source SettingsSource) map[string]string {
	view := source.Settings(ctx)
	values := make(map[string]string, len(view.Items))
	for _, item := range view.Items {
		values[item.Key] = item.Value
	}

	return values
}

// submittedSettingValue reads the value a settings form submitted for key. An
// unchecked boolean checkbox submits no "value:<key>" at all, so the "bool:<key>"
// marker coerces that absence to an explicit "false" without any client script.
func submittedSettingValue(r *http.Request, key string) string {
	value := strings.TrimSpace(r.PostFormValue("value:" + key))
	if r.PostFormValue("bool:"+key) == "1" && value != "true" {
		return "false"
	}

	return value
}
