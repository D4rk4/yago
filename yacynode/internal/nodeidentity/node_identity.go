// Package nodeidentity holds this node's self-description: the hash, network, and
// seed attributes that identify it, and the rule for whether a peer request
// addresses it.
package nodeidentity

import (
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
)

type Identity struct {
	Hash        yacymodel.Hash
	NetworkName string
	Name        string
	Host        string
	Port        int
	Flags       yacymodel.Flags
	Version     string
	Start       time.Time
}

func (id Identity) Uptime(now time.Time) int {
	return int(now.Sub(id.Start).Minutes())
}

func (id Identity) NetworkMatches(network string) bool {
	return yacyproto.NetworkUnit(network) == yacyproto.NetworkUnit(id.NetworkName)
}

func (id Identity) Addresses(network string, youare yacymodel.Hash) bool {
	return id.NetworkMatches(network) && youare == id.Hash
}
