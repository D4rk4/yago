package dhtexchange

import (
	"context"

	"github.com/D4rk4/yago/yagomodel"
)

type OutboundWordRestorer interface {
	RestoreOutboundWords(context.Context, []yagomodel.WordPostings) (int, error)
}

func outboundRestoreWords(postings []yagomodel.RWIPosting) []yagomodel.WordPostings {
	positions := make(map[yagomodel.Hash]int)
	words := make([]yagomodel.WordPostings, 0)
	for _, posting := range postings {
		position, known := positions[posting.WordHash]
		if !known {
			position = len(words)
			positions[posting.WordHash] = position
			words = append(words, yagomodel.WordPostings{WordHash: posting.WordHash})
		}
		words[position].Postings = append(words[position].Postings, posting)
	}

	return words
}
