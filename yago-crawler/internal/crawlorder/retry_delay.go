package crawlorder

import (
	cryptorand "crypto/rand"
	"io"
	"math/big"
	"time"
)

func jitteredRetryWait(wait time.Duration, entropy io.Reader) time.Duration {
	half := wait / 2
	offset, err := cryptorand.Int(entropy, big.NewInt(int64(wait-half)))
	if err != nil {
		return half
	}

	return half + time.Duration(offset.Int64())
}
