package yagonode

import "github.com/D4rk4/yago/yagonode/internal/searchcore"

func humanSearchPartialFailures(
	failures []searchcore.PartialFailure,
) []searchcore.PartialFailure {
	human := make([]searchcore.PartialFailure, len(failures))
	for index, failure := range failures {
		human[index] = failure
		human[index].Source = failure.SourceLabel()
	}

	return human
}
