package services

import "github.com/nikitakarpei/yacy-rwi-node/internal/core/contracts"

var (
	_ contracts.RuntimeStatus = RuntimeStatus{}
	_ contracts.PeerDirectory = (*PeerDirectory)(nil)
	_ contracts.RWIReceiver   = RWIReceiver{}
	_ contracts.URLReceiver   = URLReceiver{}
	_ contracts.Searcher      = Searcher{}
	_ contracts.Counter       = Counter{}
)
