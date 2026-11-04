package pipeline

import (
	"crypto/rand"
	"math/big"
	"time"
)

var pageRetryJitterRandomInt = func(maximum *big.Int) (*big.Int, error) {
	return rand.Int(rand.Reader, maximum)
}

func securePageRetryJitter(maximum time.Duration) (time.Duration, error) {
	value, err := pageRetryJitterRandomInt(big.NewInt(int64(maximum)))
	if err != nil {
		return 0, err
	}
	return time.Duration(value.Int64()), nil
}
