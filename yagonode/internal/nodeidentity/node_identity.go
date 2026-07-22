// Package nodeidentity holds this node's self-description: the hash, network, and
// seed attributes that identify it, and the rule for whether a peer request
// addresses it.
package nodeidentity

import (
	"net/url"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagoproto"
)

type Identity struct {
	Hash                     yagomodel.Hash
	NetworkName              string
	Name                     string
	Host                     string
	Port                     int
	Flags                    yagomodel.Flags
	Version                  string
	Start                    time.Time
	BirthDate                time.Time
	AuthenticationMode       yagoproto.NetworkAuthenticationMode
	AuthenticationEssentials string
}

func (id Identity) Uptime(now time.Time) int {
	return int(now.Sub(id.Start).Minutes())
}

// UptimeSeconds reports uptime in seconds, unlike Uptime which reports the
// minute-granularity value the YaCy seed carries. The admin console renders this
// so the figure advances on every refresh rather than only once a minute.
func (id Identity) UptimeSeconds(now time.Time) int {
	return int(now.Sub(id.Start).Seconds())
}

func (id Identity) NetworkMatches(network string) bool {
	return yagoproto.NetworkUnit(network) == yagoproto.NetworkUnit(id.NetworkName)
}

func (id Identity) Addresses(network string, youare yagomodel.Hash) bool {
	return id.NetworkMatches(network) && youare == id.Hash
}

func (id Identity) Authenticates(
	network string,
	networkPresent bool,
	key string,
	iam string,
	magic string,
) bool {
	form := url.Values{
		yagoproto.FieldKey:      {key},
		yagoproto.FieldIam:      {iam},
		yagoproto.FieldMagicMD5: {magic},
	}
	if network != "" || networkPresent {
		form.Set(yagoproto.FieldNetworkName, network)
	}

	return id.NetworkAccess().Authorizes(form)
}

func (id Identity) NetworkAccess() yagoproto.NetworkAccess {
	return yagoproto.NetworkAccess{
		NetworkName: id.NetworkName,
		Mode:        id.AuthenticationMode,
		Essentials:  id.AuthenticationEssentials,
		Self:        id.Hash,
	}
}
