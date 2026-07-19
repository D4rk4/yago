package yagonode

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/adminui"
)

const (
	storedBindingsUnavailable = "Stored listen addresses are unavailable."
	hostInterfacesUnavailable = "Host network interfaces are unavailable."
)

func (s *bindingSource) bindingsView(ctx context.Context) adminui.BindingsView {
	overrides, err := s.store.All(ctx)
	if err != nil {
		return adminui.BindingsView{Error: storedBindingsUnavailable}
	}
	addresses, err := discoverBindAddresses(s.interfaces)
	if err != nil {
		return adminui.BindingsView{Error: hostInterfacesUnavailable}
	}

	options := bindInterfaceOptions(addresses)
	view := adminui.BindingsView{Items: make([]adminui.BindItem, 0, len(bindDefinitions()))}
	for _, definition := range bindDefinitions() {
		item, err := s.bindingItem(definition, options, overrides)
		if err != nil {
			return adminui.BindingsView{Error: storedBindingsUnavailable}
		}
		view.Items = append(view.Items, item)
	}

	return view
}

func (s *bindingSource) bindingItem(
	definition bindDefinition,
	options []adminui.BindInterface,
	overrides map[string]string,
) (adminui.BindItem, error) {
	environmentAddress := definition.current(s.envConfig)
	address := environmentAddress
	overridden := false
	if stored, set := overrides[definition.key]; set {
		resolved, err := parseBindOverride(definition, stored)
		if err != nil {
			return adminui.BindItem{}, fmt.Errorf(
				"decode stored bind %q: %w",
				definition.key,
				err,
			)
		}
		address, overridden = resolved, true
	}
	if definition.optional && address == "" {
		item := adminui.BindItem{
			Key:         definition.key,
			Title:       definition.title,
			Description: definition.description,
			CanDisable:  true,
			Overridden:  overridden,
			Interfaces:  options,
		}
		if overridden && environmentAddress != "" {
			host, port, err := splitBindAddr(environmentAddress)
			if err != nil {
				return adminui.BindItem{}, fmt.Errorf(
					"decode configured bind %q: %w",
					definition.key,
					err,
				)
			}
			item.Host = host
			item.Port = fmt.Sprintf("%d", port)
		}

		return item, nil
	}

	host, port, err := splitBindAddr(address)
	if err != nil {
		return adminui.BindItem{}, fmt.Errorf(
			"decode configured bind %q: %w",
			definition.key,
			err,
		)
	}

	return adminui.BindItem{
		Key:             definition.key,
		Title:           definition.title,
		Description:     definition.description,
		Host:            host,
		Port:            fmt.Sprintf("%d", port),
		ListenerEnabled: true,
		CanDisable:      definition.optional,
		Overridden:      overridden,
		Interfaces:      options,
	}, nil
}
