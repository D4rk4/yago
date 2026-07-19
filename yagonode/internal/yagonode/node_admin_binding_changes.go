package yagonode

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/adminui"
)

const (
	bindingInvalidAddressMessage          = "Choose a listed interface address and a port between 1 and 65535."
	bindingRequiredListenerDisableMessage = "This listener cannot be disabled."
	bindingUnknownActionMessage           = "Unknown binding action."
	bindingUnknownSurfaceMessage          = "Unknown surface."
)

func (s *bindingSource) applyBindingChange(
	ctx context.Context,
	definition bindDefinition,
	change adminui.BindChange,
) (adminui.BindResult, error) {
	switch change.Action {
	case "", adminui.BindActionSet:
		return s.setBinding(ctx, definition, change)
	case adminui.BindActionDisable:
		return s.disableBinding(ctx, definition)
	case adminui.BindActionReset:
		return s.resetBinding(ctx, definition)
	default:
		return adminui.BindResult{Message: bindingUnknownActionMessage}, nil
	}
}

func (s *bindingSource) setBinding(
	ctx context.Context,
	definition bindDefinition,
	change adminui.BindChange,
) (adminui.BindResult, error) {
	address, ok := s.validatedBindAddr(change)
	if !ok {
		return adminui.BindResult{
			Message: bindingInvalidAddressMessage,
		}, nil
	}

	if err := s.store.Set(ctx, definition.key, address); err != nil {
		return adminui.BindResult{}, fmt.Errorf("store bind %q: %w", definition.key, err)
	}

	s.record(definition, address)

	return adminui.BindResult{
		OK:      true,
		Message: definition.title + " will bind to " + address + " after the next restart.",
	}, nil
}

func (s *bindingSource) disableBinding(
	ctx context.Context,
	definition bindDefinition,
) (adminui.BindResult, error) {
	if !definition.optional {
		return adminui.BindResult{Message: bindingRequiredListenerDisableMessage}, nil
	}
	if err := s.store.Set(ctx, definition.key, disabledBindOverride); err != nil {
		return adminui.BindResult{}, fmt.Errorf("store bind %q: %w", definition.key, err)
	}

	s.record(definition, disabledBindOverride)

	return adminui.BindResult{
		OK:      true,
		Message: definition.title + " will be disabled after the next restart.",
	}, nil
}

func (s *bindingSource) resetBinding(
	ctx context.Context,
	definition bindDefinition,
) (adminui.BindResult, error) {
	if err := s.store.Unset(ctx, definition.key); err != nil {
		return adminui.BindResult{}, fmt.Errorf("clear bind %q: %w", definition.key, err)
	}

	s.record(definition, "environment")

	return adminui.BindResult{
		OK: true,
		Message: definition.title +
			" will use the environment listen address after the next restart.",
	}, nil
}
