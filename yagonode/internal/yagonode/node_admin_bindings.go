package yagonode

import (
	"context"
	"fmt"
	"net"

	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/events"
	"github.com/D4rk4/yago/yagonode/internal/settingsstore"
)

// bindingSource adapts the durable settings store and the bind whitelist to the
// console's per-surface bind editor. It enumerates the host's interfaces, only
// admits a host the machine actually has (which also blocks binding a surface to
// an unreachable address), and persists changes as restart-required overrides.
type bindingSource struct {
	store      *settingsstore.Store
	envConfig  nodeConfig
	interfaces func() ([]net.Addr, error)
	recorder   *events.Recorder
}

func newBindingSource(
	store *settingsstore.Store,
	envConfig nodeConfig,
	recorder *events.Recorder,
) *bindingSource {
	return &bindingSource{
		store:      store,
		envConfig:  envConfig,
		interfaces: net.InterfaceAddrs,
		recorder:   recorder,
	}
}

func (s *bindingSource) Bindings(ctx context.Context) adminui.BindingsView {
	addresses, err := discoverBindAddresses(s.interfaces)
	view := adminui.BindingsView{}
	if err != nil {
		view.Error = "Could not read the host's network interfaces."
		addresses = []bindAddress{{host: "", label: bindAllInterfacesLabel}}
	}

	options := bindInterfaceOptions(addresses)
	for _, def := range bindDefinitions() {
		view.Items = append(view.Items, s.item(ctx, def, options))
	}

	return view
}

func (s *bindingSource) item(
	ctx context.Context,
	def bindDefinition,
	options []adminui.BindInterface,
) adminui.BindItem {
	addr := def.current(s.envConfig)
	overridden := false

	if stored, set, err := s.store.Get(ctx, def.key); err == nil && set {
		if host, port, splitErr := splitBindAddr(stored); splitErr == nil {
			addr, overridden = formatBindAddr(host, port), true
		}
	}

	host, port, err := splitBindAddr(addr)
	portText := ""
	if err == nil {
		portText = fmt.Sprintf("%d", port)
	}

	return adminui.BindItem{
		Key:         def.key,
		Title:       def.title,
		Description: def.description,
		Host:        host,
		Port:        portText,
		Overridden:  overridden,
		Interfaces:  options,
	}
}

func (s *bindingSource) UpdateBinding(
	ctx context.Context,
	change adminui.BindChange,
) (adminui.BindResult, error) {
	def, ok := indexBindDefinitions()[change.Key]
	if !ok {
		return adminui.BindResult{Message: "Unknown surface."}, nil
	}

	addr, ok := s.validatedBindAddr(change)
	if !ok {
		return adminui.BindResult{
			Message: "Choose a listed interface address and a port between 1 and 65535.",
		}, nil
	}

	if err := s.store.Set(ctx, def.key, addr); err != nil {
		return adminui.BindResult{}, fmt.Errorf("store bind %q: %w", def.key, err)
	}

	s.record(def, addr)

	return adminui.BindResult{
		OK:      true,
		Message: def.title + " will bind to " + addr + " after the next restart.",
	}, nil
}

func (s *bindingSource) validatedBindAddr(change adminui.BindChange) (string, bool) {
	port, err := parseBindPort(change.Port)
	if err != nil {
		return "", false
	}
	if !s.hostIsLocal(change.Host) {
		return "", false
	}

	return formatBindAddr(change.Host, port), true
}

func (s *bindingSource) hostIsLocal(host string) bool {
	addresses, err := discoverBindAddresses(s.interfaces)
	if err != nil {
		return host == ""
	}
	for _, address := range addresses {
		if address.host == host {
			return true
		}
	}

	return false
}

func (s *bindingSource) record(def bindDefinition, addr string) {
	if s.recorder == nil {
		return
	}

	s.recorder.Record(
		events.SeverityInfo,
		events.CategoryConfig,
		"bind.updated",
		fmt.Sprintf("surface %q bind set to %s (restart required)", def.key, addr),
	)
}

func bindInterfaceOptions(addresses []bindAddress) []adminui.BindInterface {
	out := make([]adminui.BindInterface, 0, len(addresses))
	for _, address := range addresses {
		out = append(out, adminui.BindInterface{Value: address.host, Label: address.label})
	}

	return out
}
