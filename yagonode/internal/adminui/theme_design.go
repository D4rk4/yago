package adminui

import (
	"context"
	"fmt"
	"net/http"
)

// Theme document names shared with the portal theme store by value, so the
// console does not depend on the store package.
const (
	themePageSearch   = "search"
	themePageResults  = "results"
	themeSharedStyles = "styles"
)

// ThemeDocument is one operator theme document as the design tabs see it: the
// body plus the parse status recorded at save time.
type ThemeDocument struct {
	Body       string
	ParseOK    bool
	ParseError string
}

// ThemeStore persists the operator portal theme (ADR-0033): two Handlebars page
// templates and a shared styles block, plus the enabled toggle. The node wires
// it to the portaltheme vault store; a nil store renders the design tabs as
// placeholders.
type ThemeStore interface {
	Enabled() bool
	SetEnabled(ctx context.Context, enabled bool) error
	Document(ctx context.Context, page string) (ThemeDocument, bool, error)
	SaveDocument(ctx context.Context, page, body string) (ThemeDocument, error)
	ResetDocument(ctx context.Context, page string) (bool, error)
	DefaultBody(page string) string
}

// designFormData feeds one design tab's editor form: the page template being
// edited plus the shared styles block and the theme toggle.
type designFormData struct {
	CSRF    string
	Enabled bool
	Page    string
	Label   string
	Doc     ThemeDocument
	// Overridden reports whether the operator saved a custom body, so the
	// Default design button only shows when there is something to reset.
	Overridden       bool
	Styles           ThemeDocument
	StylesOverridden bool
}

// portalDesignForms builds both design tabs' form data from the theme store; a
// page without a stored override edits its built-in default body.
func (c *Console) portalDesignForms(
	ctx context.Context,
	csrf string,
) (search, results *designFormData, err error) {
	styles, stylesOverridden, err := c.themeDocument(ctx, themeSharedStyles)
	if err != nil {
		return nil, nil, err
	}
	enabled := c.theme.Enabled()
	build := func(page, label string) (*designFormData, error) {
		doc, overridden, err := c.themeDocument(ctx, page)
		if err != nil {
			return nil, err
		}

		return &designFormData{
			CSRF:             csrf,
			Enabled:          enabled,
			Page:             page,
			Label:            label,
			Doc:              doc,
			Overridden:       overridden,
			Styles:           styles,
			StylesOverridden: stylesOverridden,
		}, nil
	}
	if search, err = build(themePageSearch, "search page"); err != nil {
		return nil, nil, err
	}
	if results, err = build(themePageResults, "results page"); err != nil {
		return nil, nil, err
	}

	return search, results, nil
}

// themeDocument loads a stored document or falls back to its built-in default
// body, reporting whether the operator overrode it.
func (c *Console) themeDocument(
	ctx context.Context,
	page string,
) (ThemeDocument, bool, error) {
	doc, found, err := c.theme.Document(ctx, page)
	if err != nil {
		return ThemeDocument{}, false, fmt.Errorf("load theme document %q: %w", page, err)
	}
	if !found {
		return ThemeDocument{Body: c.theme.DefaultBody(page), ParseOK: true}, false, nil
	}

	return doc, true, nil
}

// handlePortalDesign applies one design tab's submit: save the tab's template
// plus the shared styles and the toggle, or restore a built-in default.
func (c *Console) handlePortalDesign(w http.ResponseWriter, r *http.Request) {
	if c.settings == nil || c.theme == nil {
		http.NotFound(w, r)

		return
	}
	page := r.PostFormValue("page")
	if page != themePageSearch && page != themePageResults {
		http.NotFound(w, r)

		return
	}
	notice, errMsg := c.applyPortalDesign(r, page)
	c.renderPortalPage(w, r, notice, errMsg)
}

func (c *Console) applyPortalDesign(r *http.Request, page string) (notice, errMsg string) {
	ctx := r.Context()
	switch r.PostFormValue("action") {
	case "default":
		if _, err := c.theme.ResetDocument(ctx, page); err != nil {
			return "", "Restoring the default design failed: " + err.Error()
		}

		return "Default design restored.", ""
	case "default-styles":
		if _, err := c.theme.ResetDocument(ctx, themeSharedStyles); err != nil {
			return "", "Restoring the default styles failed: " + err.Error()
		}

		return "Default styles restored.", ""
	default:
		return c.savePortalDesign(ctx, page, r)
	}
}

func (c *Console) savePortalDesign(
	ctx context.Context,
	page string,
	r *http.Request,
) (notice, errMsg string) {
	doc, err := c.theme.SaveDocument(ctx, page, r.PostFormValue("body"))
	if err != nil {
		return "", "Saving the design failed: " + err.Error()
	}
	if _, err := c.theme.SaveDocument(
		ctx,
		themeSharedStyles,
		r.PostFormValue("styles"),
	); err != nil {
		return "", "Saving the shared styles failed: " + err.Error()
	}
	if err := c.theme.SetEnabled(ctx, r.PostFormValue("enabled") == "true"); err != nil {
		return "", "Switching the theme failed: " + err.Error()
	}
	if !doc.ParseOK {
		return "", "Saved, but the template does not parse: " + doc.ParseError +
			" The public portal keeps serving the previous design."
	}

	return "Design saved.", ""
}
