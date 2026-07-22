package peerannouncement

import "github.com/D4rk4/yago/yagomodel"

func responderAtTargetEndpoint(
	responder yagomodel.Seed,
	target yagomodel.Seed,
	contactedHost yagomodel.Optional[yagomodel.Host],
) yagomodel.Seed {
	verified := responder.Copy()
	contacted := targetAtContactedHost(target, contactedHost)
	verified.Hash = contacted.Hash
	verified.IP = contacted.IP
	verified.IP6 = contacted.IP6
	verified.Port = contacted.Port
	verified.PortSSL = contacted.PortSSL

	return verified
}

func targetAtContactedHost(
	target yagomodel.Seed,
	contactedHost yagomodel.Optional[yagomodel.Host],
) yagomodel.Seed {
	contacted := target.Copy()
	if host, ok := contactedHost.Get(); ok {
		contacted = contacted.WithPrimaryHost(host)
	}

	return contacted
}
