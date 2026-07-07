package searchcore

import (
	"fmt"
	"regexp"
)

const DefaultPublicLimit = 10

func NormalizePublicRequest(req Request, limitCap int) (Request, error) {
	if limitCap <= 0 {
		limitCap = DefaultPublicLimit
	}
	if req.Source == "" {
		// YaCy defaults resource to global: a public search consults the swarm
		// unless the caller explicitly narrows to the local index. Defaulting
		// to local silently cut every /yacysearch.* query without resource=
		// off from peers (SEARCH-36).
		req.Source = SourceGlobal
	}
	if req.ContentDomain == "" {
		req.ContentDomain = ContentDomainText
	}
	if req.Verify == "" {
		// YaCy defaults search.verify to "ifexist": results are checked against
		// the evidence at hand unless the caller opts out with verify=false.
		req.Verify = VerifyIfExist
	}
	if req.Limit <= 0 {
		req.Limit = DefaultPublicLimit
	}
	if req.Limit > limitCap {
		req.Limit = limitCap
	}
	if req.Offset < 0 {
		return Request{}, fmt.Errorf("negative offset")
	}
	if err := validateSource(req.Source); err != nil {
		return Request{}, err
	}
	if err := validateContentDomain(req.ContentDomain); err != nil {
		return Request{}, err
	}
	if err := validateVerifyMode(req.Verify); err != nil {
		return Request{}, err
	}
	if err := validateFilter("urlmaskfilter", req.URLMaskFilter); err != nil {
		return Request{}, err
	}
	if err := validateFilter("prefermaskfilter", req.PreferMaskFilter); err != nil {
		return Request{}, err
	}

	return req, nil
}

func validateSource(source Source) error {
	switch source {
	case SourceLocal, SourceGlobal:
		return nil
	default:
		return fmt.Errorf("unsupported source %q", source)
	}
}

func validateContentDomain(domain ContentDomain) error {
	switch domain {
	case ContentDomainAll,
		ContentDomainText,
		ContentDomainImage,
		ContentDomainAudio,
		ContentDomainVideo,
		ContentDomainApp:
		return nil
	default:
		return fmt.Errorf("unsupported content domain %q", domain)
	}
}

func validateVerifyMode(mode VerifyMode) error {
	switch mode {
	case VerifyFalse, VerifyTrue, VerifyCacheOnly, VerifyIfFresh, VerifyIfExist:
		return nil
	default:
		return fmt.Errorf("unsupported verify mode %q", mode)
	}
}

func validateFilter(name, value string) error {
	if value == "" {
		return nil
	}
	if _, err := regexp.Compile(value); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}

	return nil
}
