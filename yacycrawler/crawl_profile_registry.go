package yacycrawler

import (
	"fmt"
	"regexp"
	"sync"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
)

type CompiledProfile struct {
	Profile      yacycrawlcontract.CrawlProfile
	mustMatch    *regexp.Regexp
	mustNotMatch *regexp.Regexp
}

func (c CompiledProfile) URLAllowed(rawURL string) bool {
	if c.mustMatch != nil && !c.mustMatch.MatchString(rawURL) {
		return false
	}
	if c.mustNotMatch != nil && c.mustNotMatch.MatchString(rawURL) {
		return false
	}
	return true
}

type CrawlProfileRegistry struct {
	mu       sync.Mutex
	profiles map[string]CompiledProfile
}

func NewCrawlProfileRegistry() *CrawlProfileRegistry {
	return &CrawlProfileRegistry{profiles: make(map[string]CompiledProfile)}
}

func (r *CrawlProfileRegistry) Register(profile yacycrawlcontract.CrawlProfile) error {
	compiled, err := compileProfile(profile)
	if err != nil {
		return err
	}
	r.mu.Lock()
	r.profiles[profile.Handle] = compiled
	r.mu.Unlock()
	return nil
}

func (r *CrawlProfileRegistry) Lookup(handle string) (CompiledProfile, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	compiled, ok := r.profiles[handle]
	return compiled, ok
}

func compileProfile(profile yacycrawlcontract.CrawlProfile) (CompiledProfile, error) {
	compiled := CompiledProfile{Profile: profile}
	if profile.URLMustMatch != "" && profile.URLMustMatch != yacycrawlcontract.MatchAll {
		re, err := regexp.Compile(profile.URLMustMatch)
		if err != nil {
			return CompiledProfile{}, fmt.Errorf("compile URLMustMatch: %w", err)
		}
		compiled.mustMatch = re
	}
	if profile.URLMustNotMatch != "" {
		re, err := regexp.Compile(profile.URLMustNotMatch)
		if err != nil {
			return CompiledProfile{}, fmt.Errorf("compile URLMustNotMatch: %w", err)
		}
		compiled.mustNotMatch = re
	}
	return compiled, nil
}
