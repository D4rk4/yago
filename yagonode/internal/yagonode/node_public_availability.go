package yagonode

import (
	"github.com/D4rk4/yago/yagonode/internal/publicportal"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func portalSearchAvailability(response searchcore.Response) publicportal.SearchAvailability {
	return publicportal.SearchAvailability{
		Materialized: response.Availability.Materialized,
		Exhausted:    response.Availability.Exhausted,
	}
}
