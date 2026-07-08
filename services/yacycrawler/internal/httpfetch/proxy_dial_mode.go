package httpfetch

type ProxyDialMode int

const (
	ProxyDialTunnel ProxyDialMode = iota
	ProxyDialAbsoluteURL
)
