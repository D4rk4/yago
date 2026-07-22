package indextransfer

import "github.com/D4rk4/yago/yagomodel"

type rejectedURLSet map[yagomodel.Hash]struct{}

func newRejectedURLs(urls []yagomodel.Hash) rejectedURLSet {
	rejected := make(rejectedURLSet, len(urls))
	rejected.add(urls)

	return rejected
}

func (r rejectedURLSet) add(urls []yagomodel.Hash) {
	for _, url := range urls {
		r[url] = struct{}{}
	}
}

func (r rejectedURLSet) without(urls []yagomodel.Hash) []yagomodel.Hash {
	accepted := make([]yagomodel.Hash, 0, len(urls))
	for _, url := range urls {
		if _, rejected := r[url]; !rejected {
			accepted = append(accepted, url)
		}
	}

	return accepted
}

func (r rejectedURLSet) postings(
	postings []yagomodel.RWIPosting,
) []yagomodel.RWIPosting {
	rejected := make([]yagomodel.RWIPosting, 0)
	for _, posting := range postings {
		url, err := posting.URLHash()
		if err != nil {
			continue
		}
		if _, found := r[url.Hash()]; found {
			rejected = append(rejected, posting)
		}
	}

	return rejected
}

func missingMetadataURLs(
	requested []yagomodel.Hash,
	rows []yagomodel.URIMetadataRow,
) []yagomodel.Hash {
	available := make(map[yagomodel.Hash]struct{}, len(rows))
	for _, row := range rows {
		url, err := row.URLHash()
		if err == nil {
			available[url.Hash()] = struct{}{}
		}
	}

	missing := make([]yagomodel.Hash, 0)
	for _, url := range requested {
		if _, found := available[url]; !found {
			missing = append(missing, url)
		}
	}

	return missing
}
