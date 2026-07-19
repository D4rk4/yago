package yagonode

import "fmt"

const defaultCrawlerNodeStateMaximumBytes = "4GB"

func loadCrawlerNodeStateMaximum(getenv func(string) string) (int64, error) {
	maximumBytes, err := parseByteSize(envWithDefault(
		getenv,
		envCrawlerNodeStateMaximumBytes,
		defaultCrawlerNodeStateMaximumBytes,
	))
	if err != nil {
		return 0, fmt.Errorf("%s: %w", envCrawlerNodeStateMaximumBytes, err)
	}

	return maximumBytes, nil
}
