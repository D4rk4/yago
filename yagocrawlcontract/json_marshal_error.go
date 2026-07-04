package yagocrawlcontract

import "fmt"

func wrapMarshalError(operation string, err error) error {
	if err != nil {
		return fmt.Errorf("%s: %w", operation, err)
	}

	return nil
}
