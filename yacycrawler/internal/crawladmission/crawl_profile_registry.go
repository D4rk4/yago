package crawladmission

import (
	"fmt"
	"regexp"

	"github.com/D4rk4/yago/yacycrawlcontract"
)

type AdmissionProfile struct {
	Profile      yacycrawlcontract.CrawlProfile
	mustMatch    *regexp.Regexp
	mustNotMatch *regexp.Regexp
}

func (c AdmissionProfile) URLAllowed(rawURL string) bool {
	if c.mustMatch != nil && !c.mustMatch.MatchString(rawURL) {
		return false
	}
	if c.mustNotMatch != nil && c.mustNotMatch.MatchString(rawURL) {
		return false
	}
	return true
}

func CompileProfile(profile yacycrawlcontract.CrawlProfile) (AdmissionProfile, error) {
	compiled := AdmissionProfile{Profile: profile}
	if profile.URLMustMatch != "" && profile.URLMustMatch != yacycrawlcontract.MatchAll {
		re, err := regexp.Compile(profile.URLMustMatch)
		if err != nil {
			return AdmissionProfile{}, fmt.Errorf("compile URLMustMatch: %w", err)
		}
		compiled.mustMatch = re
	}
	if profile.URLMustNotMatch != "" {
		re, err := regexp.Compile(profile.URLMustNotMatch)
		if err != nil {
			return AdmissionProfile{}, fmt.Errorf("compile URLMustNotMatch: %w", err)
		}
		compiled.mustNotMatch = re
	}
	return compiled, nil
}
