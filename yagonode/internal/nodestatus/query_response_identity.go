package nodestatus

import (
	"strconv"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/nodeidentity"
)

const queryTimeLayout = "20060102150405"

func queryMagic(identity nodeidentity.Identity) string {
	if identity.Start.IsZero() {
		return "0"
	}

	return strconv.FormatInt(identity.Start.UnixMilli(), 10)
}

func (e queryEndpoint) currentTime() time.Time {
	if e.now == nil {
		return time.Now()
	}

	return e.now()
}
