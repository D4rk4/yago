package core

import "github.com/nikitakarpei/yacy-rwi-node/yacymodel"

type Identity interface {
	Hash() yacymodel.Hash
	NetworkName() string
}
