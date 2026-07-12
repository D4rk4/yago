package yagonode

import (
	"context"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func TestWithParsedQueryRejectsUnboundedInput(t *testing.T) {
	searcher := withParsedQuery(&recordingSearcher{})
	cases := []searchcore.Request{
		{Query: strings.Repeat("я", 513)},
		{Query: "bounded", Terms: make([]string, 33)},
	}
	for _, request := range cases {
		if _, err := searcher.Search(context.Background(), request); err == nil {
			t.Fatalf("request = %#v", request)
		}
	}
}
