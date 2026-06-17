package services

import "github.com/nikitakarpei/yacy-rwi-node/yacymodel"

type Identity struct {
	hash        yacymodel.Hash
	networkName string
	name        string
	host        string
	port        int
	flags       yacymodel.Flags
}

func NewIdentity(
	hash yacymodel.Hash,
	networkName, name, host string,
	port int,
	flags yacymodel.Flags,
) Identity {
	return Identity{
		hash:        hash,
		networkName: networkName,
		name:        name,
		host:        host,
		port:        port,
		flags:       flags,
	}
}

func (i Identity) Hash() yacymodel.Hash { return i.hash }

func (i Identity) NetworkName() string { return i.networkName }
