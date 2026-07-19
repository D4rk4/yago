package urldenylist

import "slices"

func (s Snapshot) Values() ([]string, []string) {
	urls := make([]string, 0, len(s.urls))
	for value := range s.urls {
		urls = append(urls, value)
	}
	domains := make([]string, 0, len(s.domains))
	for value := range s.domains {
		domains = append(domains, value)
	}
	slices.Sort(urls)
	slices.Sort(domains)

	return urls, domains
}
