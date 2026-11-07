package pageindex

import "github.com/D4rk4/yago/yagomodel"

func addURLComponentCountToPostings(postings []yagomodel.RWIPosting, rawURL string) {
	componentCount := componentCardinal(urlComponentCount(rawURL))
	for index := range postings {
		if postings[index].Properties == nil {
			postings[index].Properties = make(map[string]string, 1)
		}
		postings[index].Properties[yagomodel.ColURLComponentCount] = componentCount
	}
}
