package crawladmission

import (
	"fmt"
	"regexp"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

type AdmissionProfile struct {
	Profile           yagocrawlcontract.CrawlProfile
	mustMatch         *regexp.Regexp
	mustNotMatch      *regexp.Regexp
	indexMustMatch    *regexp.Regexp
	indexMustNotMatch *regexp.Regexp
}

func (c AdmissionProfile) URLAllowed(rawURL string) bool {
	return matchAllows(c.mustMatch, c.mustNotMatch, rawURL)
}

func (c AdmissionProfile) IndexAllowed(rawURL string) bool {
	return matchAllows(c.indexMustMatch, c.indexMustNotMatch, rawURL)
}

func matchAllows(mustMatch, mustNotMatch *regexp.Regexp, rawURL string) bool {
	if mustMatch != nil && !mustMatch.MatchString(rawURL) {
		return false
	}
	if mustNotMatch != nil && mustNotMatch.MatchString(rawURL) {
		return false
	}

	return true
}

func CompileProfile(profile yagocrawlcontract.CrawlProfile) (AdmissionProfile, error) {
	if err := profile.ValidateMaxPagesPerRun(); err != nil {
		return AdmissionProfile{}, fmt.Errorf("validate crawl profile page budget: %w", err)
	}
	compiled := AdmissionProfile{Profile: profile}

	var err error
	if compiled.mustMatch, err = compilePattern(
		"URLMustMatch",
		profile.URLMustMatch,
		true,
	); err != nil {
		return AdmissionProfile{}, err
	}
	if compiled.mustNotMatch, err = compilePattern(
		"URLMustNotMatch",
		profile.URLMustNotMatch,
		false,
	); err != nil {
		return AdmissionProfile{}, err
	}
	if compiled.indexMustMatch, err = compilePattern(
		"IndexURLMustMatch", profile.IndexURLMustMatch, true,
	); err != nil {
		return AdmissionProfile{}, err
	}
	if compiled.indexMustNotMatch, err = compilePattern(
		"IndexURLMustNotMatch", profile.IndexURLMustNotMatch, false,
	); err != nil {
		return AdmissionProfile{}, err
	}

	return compiled, nil
}

func compilePattern(field, pattern string, skipMatchAll bool) (*regexp.Regexp, error) {
	if pattern == "" || (skipMatchAll && pattern == yagocrawlcontract.MatchAll) {
		return nil, nil
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("compile %s: %w", field, err)
	}

	return re, nil
}
