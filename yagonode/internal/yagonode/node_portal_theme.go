package yagonode

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/events"
	"github.com/D4rk4/yago/yagonode/internal/portaltheme"
)

// themeEventSink adapts the node event recorder for the portal theme store; a
// nil recorder (bare test assemblies) degrades to a no-op sink so theme writes
// never have to care.
func themeEventSink(recorder *events.Recorder) portaltheme.EventSink {
	if recorder == nil {
		return noopThemeEvents{}
	}

	return recorder
}

type noopThemeEvents struct{}

func (noopThemeEvents) Record(
	severity events.Severity,
	category events.Category,
	name string,
	message string,
) {
	_, _, _, _ = severity, category, name, message
}

// portalThemeAdmin adapts the portal theme store to the admin console's
// ThemeStore port, converting the stored document into the console's view.
type portalThemeAdmin struct {
	theme *portaltheme.Theme
}

// newPortalThemeAdmin wraps the theme store for the console; a nil store yields
// a nil port so the design tabs render as placeholders.
func newPortalThemeAdmin(theme *portaltheme.Theme) adminui.ThemeStore {
	if theme == nil {
		return nil
	}

	return portalThemeAdmin{theme: theme}
}

func (a portalThemeAdmin) Enabled() bool { return a.theme.Enabled() }

func (a portalThemeAdmin) SetEnabled(ctx context.Context, enabled bool) error {
	if err := a.theme.SetEnabled(ctx, enabled); err != nil {
		return fmt.Errorf("toggle portal theme: %w", err)
	}

	return nil
}

func (a portalThemeAdmin) Document(
	ctx context.Context,
	page string,
) (adminui.ThemeDocument, bool, error) {
	doc, found, err := a.theme.Document(ctx, page)
	if err != nil {
		return adminui.ThemeDocument{}, false, fmt.Errorf("load portal theme document: %w", err)
	}

	return adminThemeDocument(doc), found, nil
}

func (a portalThemeAdmin) SaveDocument(
	ctx context.Context,
	page, body string,
) (adminui.ThemeDocument, error) {
	doc, err := a.theme.SaveDocument(ctx, page, body)
	if err != nil {
		return adminui.ThemeDocument{}, fmt.Errorf("save portal theme document: %w", err)
	}

	return adminThemeDocument(doc), nil
}

func (a portalThemeAdmin) ResetDocument(ctx context.Context, page string) (bool, error) {
	existed, err := a.theme.ResetDocument(ctx, page)
	if err != nil {
		return false, fmt.Errorf("reset portal theme document: %w", err)
	}

	return existed, nil
}

func (a portalThemeAdmin) DefaultBody(page string) string {
	return portaltheme.DefaultBody(page)
}

func adminThemeDocument(doc portaltheme.Document) adminui.ThemeDocument {
	return adminui.ThemeDocument{
		Body:       doc.Body,
		ParseOK:    doc.ParseOK,
		ParseError: doc.ParseError,
	}
}
