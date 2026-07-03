package yacycrawlcontract

import (
	"fmt"
	"regexp"
)

const MaxCrawlDepth = 64

func (p CrawlProfile) Validate() error {
	if p.MaxDepth < 0 {
		return fmt.Errorf("maxDepth must not be negative")
	}
	if p.MaxDepth > MaxCrawlDepth {
		return fmt.Errorf("maxDepth must not exceed %d", MaxCrawlDepth)
	}
	if p.MaxPagesPerHost != UnlimitedPagesPerHost && p.MaxPagesPerHost <= 0 {
		return fmt.Errorf(
			"maxPagesPerHost must be positive or %d for unlimited",
			UnlimitedPagesPerHost,
		)
	}
	if p.RecrawlIfOlder < 0 {
		return fmt.Errorf("recrawlIfOlder must not be negative")
	}
	if p.CrawlDelay < 0 {
		return fmt.Errorf("crawlDelay must not be negative")
	}
	if err := validateURLPattern("urlMustMatch", p.URLMustMatch); err != nil {
		return err
	}
	if err := validateURLPattern("urlMustNotMatch", p.URLMustNotMatch); err != nil {
		return err
	}
	if err := validateURLPattern("indexMustMatch", p.IndexURLMustMatch); err != nil {
		return err
	}

	return validateURLPattern("indexMustNotMatch", p.IndexURLMustNotMatch)
}

func validateURLPattern(field, pattern string) error {
	if pattern == "" || pattern == MatchAll {
		return nil
	}
	if _, err := regexp.Compile(pattern); err != nil {
		return fmt.Errorf("%s is not a valid regular expression: %w", field, err)
	}

	return nil
}
