package yagonode

import "fmt"

const disabledBindOverride = "off"

func parseBindOverride(definition bindDefinition, raw string) (string, error) {
	if raw == disabledBindOverride {
		if !definition.optional {
			return "", fmt.Errorf("listener cannot be disabled")
		}

		return "", nil
	}

	host, port, err := splitBindAddr(raw)
	if err != nil {
		return "", err
	}

	return formatBindAddr(host, port), nil
}
