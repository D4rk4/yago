package pageparse

import "github.com/D4rk4/yago/yago-crawler/internal/weburl"

func ResolveLinks(base string, links []string) (local, external []string) {
	return resolveLinks(base, links, 0)
}

func resolveLinks(base string, links []string, maximum int) (local, external []string) {
	baseURL, ok := weburl.ParseBase(base)
	if !ok {
		return nil, nil
	}
	local = make([]string, 0, len(links))
	external = make([]string, 0, len(links))
	for _, raw := range links {
		if maximum > 0 && len(local)+len(external) == maximum {
			break
		}
		resolved, ok := weburl.Resolve(baseURL, raw)
		if !ok {
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
