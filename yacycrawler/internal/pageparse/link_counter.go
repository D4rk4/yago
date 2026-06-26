package pageparse

import "net/url"

func ResolveLinks(base string, links []string) (local, external []string) {
	baseURL, err := url.Parse(base)
	if err != nil {
		return nil, nil
	}
	local = make([]string, 0, len(links))
	external = make([]string, 0, len(links))
	for _, raw := range links {
		ref, err := url.Parse(raw)
		if err != nil {
			continue
		}
		resolved := baseURL.ResolveReference(ref)
		if resolved.Scheme != "http" && resolved.Scheme != "https" {
			continue
		}
		if resolved.Host == baseURL.Host {
			local = append(local, resolved.String())
			continue
		}
		external = append(external, resolved.String())
	}
	return local, external
}
