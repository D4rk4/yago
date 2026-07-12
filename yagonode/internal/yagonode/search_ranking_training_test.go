package yagonode

import (
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func TestRankingTrainingSearcherWrapsSearchFailure(t *testing.T) {
	sentinel := errors.New("search failed")
	_, err := (rankingTrainingSearcher{
		searcher: stubTuneSearcher{err: sentinel},
	}).Search(t.Context(), searchcore.Request{})
	if !errors.Is(err, sentinel) {
		t.Fatalf("error = %v", err)
	}
}
