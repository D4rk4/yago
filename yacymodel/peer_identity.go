package yacymodel

type PeerIdentity struct {
	Hash        Hash
	NetworkName string
	Name        string
	Host        string
	Port        int
	Flags       Flags
}
