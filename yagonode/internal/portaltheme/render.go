package portaltheme

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"

	"github.com/mailgun/raymond/v2"
)

// execTemplate runs a compiled template; a seam so tests can force the
// panic-recovery branch, which the curated helpers never trigger themselves.
var execTemplate = func(tpl *raymond.Template, view any) (string, error) {
	return tpl.Exec(view)
}

// Render produces the operator-themed page when the theme is enabled and the
// page's template is stored and healthy, reporting false otherwise so the
// caller serves the built-in portal. The view gains the shared styles block as
// {{{styles}}}. Every request still attempts the theme — a failure can depend
// on the particular view — but the failure log fires once per saved body, not
// per request, so a broken template cannot flood the log.
func (t *Theme) Render(ctx context.Context, page string, view map[string]any) (string, bool) {
	t.mu.RLock()
	tpl := t.compiled[page]
	enabled := t.enabled
	styles := t.styles
	t.mu.RUnlock()
	if !enabled || tpl == nil {
		return "", false
	}
	view["styles"] = styles
	html, err := safeExec(tpl, view)
	if err != nil {
		t.logRenderFailure(ctx, page, err)

		return "", false
	}

	return html, true
}

// logRenderFailure warns about a failing theme render once per saved body; the
// per-page flag resets when the operator saves or resets the document.
func (t *Theme) logRenderFailure(ctx context.Context, page string, err error) {
	t.mu.Lock()
	alreadyFailed := t.failed[page]
	t.failed[page] = true
	t.mu.Unlock()
	if alreadyFailed {
		return
	}
	slog.WarnContext(
		ctx,
		"portal theme render failed; serving the built-in portal",
		slog.String("page", page),
		slog.Any("error", err),
	)
}

// safeExec shields the portal from a panicking template evaluation: raymond
// propagates helper panics out of Exec, and the public surface must degrade to
// the default render instead of crashing the request (ADR-0033).
func safeExec(tpl *raymond.Template, view any) (html string, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("template render panic: %v", recovered)
		}
	}()

	return execTemplate(tpl, view)
}

// registerHelpers installs the curated helper allowlist (ADR-0033) on a
// compiled template: formatting, URL-encoding, pluralizing, and truncating.
// No helper touches the filesystem, network, environment, or process.
func registerHelpers(tpl *raymond.Template) {
	tpl.RegisterHelpers(map[string]any{
		"urlencode":    url.QueryEscape,
		"truncate":     truncateHelper,
		"pluralize":    pluralizeHelper,
		"formatNumber": formatNumberHelper,
	})
}

// truncateHelper shortens a string to at most limit runes, appending an
// ellipsis when it cut anything; a non-positive limit yields the ellipsis only.
func truncateHelper(value string, limit int) string {
	if limit < 0 {
		limit = 0
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}

	return string(runes[:limit]) + "…"
}

// pluralizeHelper picks the singular or plural word for a count, so templates
// can write {{pluralize results.totalResults "result" "results"}}.
func pluralizeHelper(count int, singular, plural string) string {
	if count == 1 {
		return singular
	}

	return plural
}

// formatNumberHelper renders an integer with narrow no-break-space thousands
// separators (U+202F), so large result counts read in groups without wrapping.
func formatNumberHelper(value int) string {
	digits := strconv.Itoa(value)
	negative := strings.HasPrefix(digits, "-")
	if negative {
		digits = digits[1:]
	}
	var out strings.Builder
	for i, digit := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			out.WriteRune(' ')
		}
		out.WriteRune(digit)
	}
	if negative {
		return "-" + out.String()
	}

	return out.String()
}
