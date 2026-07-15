package adminui

import "context"

// BindInterface is one selectable host address for a bindable surface. An empty
// Value binds to all interfaces.
type BindInterface struct {
	Value string
	Label string
}

// BindItem is one bindable surface: its current listen host and port and the
// host addresses discovered on the machine.
type BindItem struct {
	Key             string
	Title           string
	Description     string
	Host            string
	Port            string
	ListenerEnabled bool
	Overridden      bool
	Interfaces      []BindInterface
}

// BindingsView is the per-surface bind editor for the Configuration section. A
// non-empty Error reports why the editor is degraded (e.g. interface
// enumeration failed); bind changes always take effect after a restart.
type BindingsView struct {
	Items []BindItem
	Error string
}

// BindChange is a submitted change to one surface's listen address.
type BindChange struct {
	Key  string
	Host string
	Port string
}

// BindResult reports the outcome of a bind change. OK is false for a rejected
// change, in which case Message is a display-safe reason.
type BindResult struct {
	OK      bool
	Message string
}

// BindingSource reads and writes the per-surface listen addresses. A nil
// provider hides the bind editor. Changes are persisted as runtime overrides and
// applied on the next restart.
type BindingSource interface {
	Bindings(ctx context.Context) BindingsView
	UpdateBinding(ctx context.Context, change BindChange) (BindResult, error)
}
