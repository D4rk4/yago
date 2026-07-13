// Package portaltheme stores and renders the operator-editable public portal
// theme (ADR-0033): Handlebars templates for the search and results pages plus
// a shared styles block, persisted in the durable vault and rendered
// server-side. A theme render that is disabled, missing, unparseable, or
// failing always falls back to the built-in Go portal, so a broken operator
// template can never take the public surface down.
package portaltheme

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/mailgun/raymond/v2"

	"github.com/D4rk4/yago/yagonode/internal/events"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

// Page names the operator-editable portal documents. PageSearch and PageResults
// are full Handlebars page templates; SharedStyles is the CSS block both page
// templates can interpolate as {{{styles}}}.
const (
	PageSearch   = "search"
	PageResults  = "results"
	SharedStyles = "styles"
)

// MaxDocumentBytes caps one stored theme document, so a runaway editor payload
// cannot bloat the vault or the per-request render.
const MaxDocumentBytes = 256 << 10

const (
	docBucket    vault.Name = "portal_theme_docs"
	configBucket vault.Name = "portal_theme_config"
	configKey               = "theme"
)

// Document is one stored theme document plus its recorded parse status, so the
// admin editor can surface a broken body without the render path re-parsing it.
type Document struct {
	Body    string
	SavedAt time.Time
	ParseOK bool
	// ParseError holds the Handlebars parse failure for the editor; an empty
	// value with ParseOK false never happens for page templates.
	ParseError string
}

type themeConfig struct {
	Enabled bool
}

// EventSink receives the config events a theme write emits; *events.Recorder
// satisfies it and tests substitute a capturing fake.
type EventSink interface {
	Record(severity events.Severity, category events.Category, name, message string)
}

type documentCodec struct{}

func (documentCodec) Encode(value Document) ([]byte, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode theme document: %w", err)
	}

	return data, nil
}

func (documentCodec) Decode(data []byte) (Document, error) {
	var value Document
	if err := json.Unmarshal(data, &value); err != nil {
		return Document{}, fmt.Errorf("decode theme document: %w", err)
	}

	return value, nil
}

type configCodec struct{}

func (configCodec) Encode(value themeConfig) ([]byte, error) {
	data, _ := json.Marshal(value)

	return data, nil
}

func (configCodec) Decode(data []byte) (themeConfig, error) {
	var value themeConfig
	if err := json.Unmarshal(data, &value); err != nil {
		return themeConfig{}, fmt.Errorf("decode theme config: %w", err)
	}

	return value, nil
}

// Theme is the vault-backed store and renderer of the operator portal theme.
// All writes go through this instance, so the compiled-template cache stays
// coherent without watching the vault.
type Theme struct {
	vault  *vault.Vault
	docs   *vault.Collection[Document]
	config *vault.Collection[themeConfig]
	events EventSink

	mu       sync.RWMutex
	enabled  bool
	styles   string
	compiled map[string]*raymond.Template
	failed   map[string]bool
}

// Open registers the theme buckets on the vault and loads the stored documents
// into the render cache. The sink receives a config event per theme write.
func Open(v *vault.Vault, sink EventSink) (*Theme, error) {
	docs, err := vault.Register(v, docBucket, documentCodec{})
	if err != nil {
		return nil, fmt.Errorf("register theme documents: %w", err)
	}
	config, err := vault.Register(v, configBucket, configCodec{})
	if err != nil {
		return nil, fmt.Errorf("register theme config: %w", err)
	}
	theme := &Theme{
		vault:    v,
		docs:     docs,
		config:   config,
		events:   sink,
		compiled: map[string]*raymond.Template{},
		failed:   map[string]bool{},
	}
	if err := theme.reload(context.Background()); err != nil {
		return nil, fmt.Errorf("load stored theme: %w", err)
	}

	return theme, nil
}

// Enabled reports whether the operator theme is active for the public portal.
func (t *Theme) Enabled() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return t.enabled
}

// SetEnabled switches the operator theme on or off for the public portal.
func (t *Theme) SetEnabled(ctx context.Context, enabled bool) error {
	err := t.vault.Update(ctx, func(tx *vault.Txn) error {
		return t.config.Put(tx, vault.Key(configKey), themeConfig{Enabled: enabled})
	})
	if err != nil {
		return fmt.Errorf("store theme toggle: %w", err)
	}
	t.mu.Lock()
	t.enabled = enabled
	t.mu.Unlock()
	t.record(fmt.Sprintf("operator portal theme %s", enabledWord(enabled)))

	return nil
}

func enabledWord(enabled bool) string {
	if enabled {
		return "enabled"
	}

	return "disabled"
}

// Document returns the stored document for the page, reporting false when the
// operator has not overridden it.
func (t *Theme) Document(ctx context.Context, page string) (Document, bool, error) {
	if err := validPage(page); err != nil {
		return Document{}, false, err
	}
	var (
		doc   Document
		found bool
	)
	err := t.vault.View(ctx, func(tx *vault.Txn) error {
		var viewErr error
		doc, found, viewErr = t.docs.Get(tx, vault.Key(page))
		if viewErr != nil {
			return fmt.Errorf("read document %q: %w", page, viewErr)
		}

		return nil
	})
	if err != nil {
		return Document{}, false, fmt.Errorf("load theme document: %w", err)
	}
	if found {
		doc.Body = repairLegacyPortalDocument(page, doc.Body)
	}

	return doc, found, nil
}

// SaveDocument stores an operator document and refreshes the render cache. A
// page body that does not parse is still stored — the editor shows the failure
// and the public render falls back to the default — so an operator never loses
// work to a syntax error. The returned document carries the parse status.
func (t *Theme) SaveDocument(ctx context.Context, page, body string) (Document, error) {
	if err := validPage(page); err != nil {
		return Document{}, err
	}
	body = repairLegacyPortalDocument(page, body)
	if len(body) > MaxDocumentBytes {
		return Document{}, fmt.Errorf(
			"theme document %q is %d bytes, above the %d byte cap",
			page, len(body), MaxDocumentBytes,
		)
	}
	doc := Document{Body: body, SavedAt: time.Now().UTC(), ParseOK: true}
	if page != SharedStyles {
		if _, err := raymond.Parse(body); err != nil {
			doc.ParseOK = false
			doc.ParseError = err.Error()
		}
	}
	err := t.vault.Update(ctx, func(tx *vault.Txn) error {
		return t.docs.Put(tx, vault.Key(page), doc)
	})
	if err != nil {
		return Document{}, fmt.Errorf("store theme document: %w", err)
	}
	t.refreshPage(page, doc, true)
	t.record(fmt.Sprintf("portal theme document %q saved (%d bytes)", page, len(body)))

	return doc, nil
}

// ResetDocument drops the operator document so the page falls back to the
// built-in default, reporting whether an override existed.
func (t *Theme) ResetDocument(ctx context.Context, page string) (bool, error) {
	if err := validPage(page); err != nil {
		return false, err
	}
	var existed bool
	err := t.vault.Update(ctx, func(tx *vault.Txn) error {
		var deleteErr error
		existed, deleteErr = t.docs.Delete(tx, vault.Key(page))
		if deleteErr != nil {
			return fmt.Errorf("delete document %q: %w", page, deleteErr)
		}

		return nil
	})
	if err != nil {
		return false, fmt.Errorf("reset theme document: %w", err)
	}
	t.refreshPage(page, Document{}, false)
	t.record(fmt.Sprintf("portal theme document %q reset to default", page))

	return existed, nil
}

// reload seeds the in-memory render cache from the vault at Open time.
func (t *Theme) reload(ctx context.Context) error {
	err := t.vault.View(ctx, func(tx *vault.Txn) error {
		conf, found, err := t.config.Get(tx, vault.Key(configKey))
		if err != nil {
			return fmt.Errorf("read theme config: %w", err)
		}
		if found {
			t.enabled = conf.Enabled
		}
		for _, page := range []string{PageSearch, PageResults, SharedStyles} {
			doc, stored, err := t.docs.Get(tx, vault.Key(page))
			if err != nil {
				return fmt.Errorf("read document %q: %w", page, err)
			}
			if stored {
				doc.Body = repairLegacyPortalDocument(page, doc.Body)
				t.refreshPage(page, doc, true)
			}
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("scan stored theme: %w", err)
	}

	return nil
}

// refreshPage updates the render cache after a store write: the styles block is
// kept raw, a parsing page body is compiled, and everything else clears the
// page's compiled entry so the render falls back to the default.
func (t *Theme) refreshPage(page string, doc Document, stored bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.failed, page)
	if page == SharedStyles {
		t.styles = ""
		if stored {
			t.styles = doc.Body
		}

		return
	}
	delete(t.compiled, page)
	if stored && doc.ParseOK {
		if tpl, err := raymond.Parse(doc.Body); err == nil {
			registerHelpers(tpl)
			t.compiled[page] = tpl
		}
	}
}

func (t *Theme) record(message string) {
	t.events.Record(events.SeverityInfo, events.CategoryConfig, "portal.theme", message)
}

func validPage(page string) error {
	switch page {
	case PageSearch, PageResults, SharedStyles:
		return nil
	default:
		return fmt.Errorf("unknown theme document %q", page)
	}
}
