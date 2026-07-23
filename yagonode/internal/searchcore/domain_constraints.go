package searchcore

import (
	"net/netip"
	"net/url"
	"strings"
)

func ResultSatisfiesDomainConstraints(req Request, result Result) bool {
	host := normalizedDomainHost(result)
	for _, domain := range req.ExcludeDomains {
		if hostSatisfiesDomain(host, domain) {
			return false
		}
	}
	if len(req.IncludeDomains) == 0 {
		return true
	}
	for _, domain := range req.IncludeDomains {
		if hostSatisfiesDomain(host, domain) {
			return true
		}
	}

	return false
}

func responseSatisfyingDomainConstraints(req Request, response Response) Response {
	if len(req.IncludeDomains) == 0 && len(req.ExcludeDomains) == 0 {
		return response
	}
	results := make([]Result, 0, len(response.Results))
	for _, result := range response.Results {
		if ResultSatisfiesDomainConstraints(req, result) {
			results = append(results, result)
		}
	}
	if len(results) == len(response.Results) {
		return response
	}
	response.Results = results
	response.TotalResults = len(results)
	response.Availability.Materialized = len(results)

	return response
}

func normalizedDomainHost(result Result) string {
	parsed, err := url.Parse(result.URL)
	if err == nil {
		if host := normalizedDomainName(parsed.Hostname()); host != "" {
			return host
		}
	}

	return normalizedDomainAuthority(result.Host)
}

func normalizedDomainAuthority(value string) string {
	value = strings.TrimSpace(value)
	if address, err := netip.ParseAddr(value); err == nil {
		return strings.ToLower(address.String())
	}
	parsed, err := url.Parse("//" + value)
	if err != nil || parsed.User != nil || parsed.Path != "" ||
		parsed.RawQuery != "" || parsed.Fragment != "" {
		return ""
	}

	return normalizedDomainName(parsed.Hostname())
}

func normalizedDomainName(value string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(value)), ".")
}

func hostSatisfiesDomain(host, domain string) bool {
	domain = normalizedDomainConstraint(domain)

	return host != "" && domain != "" &&
		(host == domain || strings.HasSuffix(host, "."+domain))
}

func normalizedDomainConstraint(domain string) string {
	domain = strings.ToLower(strings.Trim(strings.TrimSpace(domain), "."))
	domain = strings.TrimPrefix(domain, "*.")
	if address, err := netip.ParseAddr(strings.Trim(domain, "[]")); err == nil {
		return address.String()
	}

	return domain
}
