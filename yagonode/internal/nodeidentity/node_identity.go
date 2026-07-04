// Package nodeidentity holds this node's self-description: the hash, network, and
// seed attributes that identify it, and the rule for whether a peer request
// addresses it.
package nodeidentity

import (
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagoproto"
)

type Identity struct {
	Hash        yagomodel.Hash
	NetworkName string
	Name        string
	Host        string
	Port        int
	Flags       yagomodel.Flags
	Version     string
	Start       time.Time
	BirthDate   time.Time
}

func (id Identity) Uptime(now time.Time) int {
	return int(now.Sub(id.Start).Minutes())
}

func (id Identity) NetworkMatches(network string) bool {
	return yagoproto.NetworkUnit(network) == yagoproto.NetworkUnit(id.NetworkName)
}

func (id Identity) Addresses(network string, youare yagomodel.Hash) bool {
	return id.NetworkMatches(network) && youare == id.Hash
}
