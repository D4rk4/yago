package searchcore

import "strings"

func (r Request) SubmittedText() string {
	if strings.TrimSpace(r.SubmittedQuery) != "" {
		return r.SubmittedQuery
	}

	return r.Query
}
