package peerprofile

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

const (
	maximumProfileProperties    = 1024
	maximumProfilePropertyBytes = 1 << 20
)

var errProfilePropertiesTooLarge = errors.New("profile properties exceed limit")

type profilePropertyBudget struct {
	properties int
	bytes      int
}

func (b *profilePropertyBudget) retain(key, value string) (Property, bool) {
	if b.properties >= maximumProfileProperties ||
		len(key) > maximumProfilePropertyBytes-b.bytes ||
		len(value) > maximumProfilePropertyBytes-b.bytes-len(key) {
		return Property{}, false
	}
	b.properties++
	b.bytes += len(key) + len(value)

	return Property{Key: strings.Clone(key), Value: strings.Clone(value)}, true
}

func parseProfileProperties(ctx context.Context, raw string) ([]Property, error) {
	properties := make([]Property, 0, min(32, maximumProfileProperties))
	budget := &profilePropertyBudget{}
	for line := range strings.Lines(raw) {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("profile parse context: %w", err)
		}
		key, value, ok := profilePropertyParts(line)
		if !ok {
			continue
		}
		property, retained := budget.retain(key, value)
		if !retained {
			return nil, errProfilePropertiesTooLarge
		}
		properties = append(properties, property)
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("profile parse context: %w", err)
	}

	return properties, nil
}
