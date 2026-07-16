package searchcore

import (
	"github.com/D4rk4/yago/yagonode/internal/querymatch"
)

func TokenMatchesTerm(token string, term string) bool {
	return querymatch.TokenMatchesTerm(token, term)
}
