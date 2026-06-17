package infrastructure

import (
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/internal/core/ports"
)

type SystemClock struct{}

func (SystemClock) Now() time.Time {
	return time.Now()
}

var _ ports.Clock = SystemClock{}
