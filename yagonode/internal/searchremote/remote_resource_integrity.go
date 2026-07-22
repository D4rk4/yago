package searchremote

import (
	"fmt"

	"github.com/D4rk4/yago/yagoproto"
)

func validateRemoteResourceIntegrity(response yagoproto.SearchResponse) error {
	if response.Count < 0 {
		return fmt.Errorf(
			"declared remote resource count %d is negative",
			response.Count,
		)
	}
	if response.InvalidResources > 0 {
		return fmt.Errorf(
			"remote response contains %d invalid resources",
			response.InvalidResources,
		)
	}

	return nil
}
