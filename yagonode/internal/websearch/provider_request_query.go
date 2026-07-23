package websearch

import (
	"net/url"
	"strconv"
	"strings"
	"unicode"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

const maximumProviderEncodedQueryBytes = 2048

func newProviderQueryForRequest(req searchcore.Request) providerQuery {
	query := newProviderQuery(req.SubmittedText())
	query.outboundText = providerTextWithIncludedDomains(
		query.outboundText,
		req.IncludeDomains,
	)
	query.acceptResults = func(results []Result) []Result {
		return verifiedWebResults(req, results)
	}
	query.cacheIdentity = providerRequestCacheIdentity(query.outboundText, req)

	return query
}

func providerRequestCacheIdentity(outboundText string, req searchcore.Request) string {
	terms := req.Terms
	if len(terms) == 0 {
		terms = searchcore.ParseTextQuery(req.Query).Terms
	}
	fields := make(
		[]string,
		0,
		10+len(terms)+len(req.ExcludedTerms)+
			len(req.IncludeDomains)+len(req.ExcludeDomains),
	)
	fields = append(fields,
		outboundText,
		string(req.Verify),
		req.InURL,
		req.FileType,
		req.SiteHost,
		req.TLD,
		strconv.Itoa(len(terms)),
	)
	fields = append(fields, terms...)
	fields = append(fields, strconv.Itoa(len(req.ExcludedTerms)))
	fields = append(fields, req.ExcludedTerms...)
	fields = append(fields, strconv.Itoa(len(req.IncludeDomains)))
	fields = append(fields, req.IncludeDomains...)
	fields = append(fields, strconv.Itoa(len(req.ExcludeDomains)))
	fields = append(fields, req.ExcludeDomains...)
	var identity strings.Builder
	for _, field := range fields {
		identity.WriteString(strconv.Itoa(len(field)))
		identity.WriteByte(':')
		identity.WriteString(field)
	}

	return identity.String()
}

func providerTextWithIncludedDomains(query string, domains []string) string {
	operators := providerSiteOperators(domains)
	if len(operators) == 0 {
		return query
	}
	constraint := operators[0]
	if len(operators) > 1 {
		constraint = "(" + strings.Join(operators, " OR ") + ")"
	}
	if query == "" {
		if len(url.QueryEscape(constraint)) <= maximumProviderEncodedQueryBytes {
			return constraint
		}

		return query
	}

	scoped := query + " " + constraint
	if len(url.QueryEscape(scoped)) > maximumProviderEncodedQueryBytes {
		return query
	}

	return scoped
}

func providerSiteOperators(domains []string) []string {
	operators := make([]string, 0, len(domains))
	seen := make(map[string]struct{}, len(domains))
	for _, domain := range domains {
		domain = providerSiteDomain(domain)
		if domain == "" {
			continue
		}
		if _, duplicate := seen[domain]; duplicate {
			continue
		}
		seen[domain] = struct{}{}
		operators = append(operators, "site:"+domain)
	}

	return operators
}

func providerSiteDomain(domain string) string {
	domain = strings.ToLower(strings.TrimSpace(domain))
	domain = strings.Trim(domain, ".")
	domain = strings.TrimPrefix(domain, "*.")
	if domain == "" {
		return ""
	}
	if strings.Contains(domain, ":") {
		return ""
	}
	for _, character := range domain {
		if unicode.IsLetter(character) || unicode.IsNumber(character) ||
			character == '.' || character == '-' || character == '_' {
			continue
		}

		return ""
	}

	return domain
}
