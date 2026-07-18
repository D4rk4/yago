package crawlorder

import "github.com/D4rk4/yago/yago-crawler/internal/crawlsettlement"

func WithTerminalSettlementOutbox(outbox crawlsettlement.Outbox) GRPCOrderReceiverOption {
	return func(config *grpcOrderReceiverConfig) {
		config.terminalSettlements = outbox
	}
}
