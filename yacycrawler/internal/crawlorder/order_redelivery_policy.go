package crawlorder

import "time"

type OrderRedeliveryPolicy struct {
	AckWait     time.Duration
	MaxAttempts int
}

func (p OrderRedeliveryPolicy) heartbeatInterval() time.Duration {
	return p.AckWait / 2
}
