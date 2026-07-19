package crawlorder

type FetchStartSession interface {
	Connected()
	Disconnected()
}

func WithFetchStartSession(session FetchStartSession) GRPCOrderReceiverOption {
	return func(config *grpcOrderReceiverConfig) {
		config.fetchStartSession = session
	}
}
