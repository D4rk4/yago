package yagonode

type webSeedPublication uint8

const (
	webSeedPublicationFailed webSeedPublication = iota
	webSeedPublicationPublished
	webSeedPublicationCoalesced
)
