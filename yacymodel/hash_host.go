package yacymodel

const hostHashLength = 6

func (h Hash) HostHash() string {
	if len(h) != HashLength {
		return ""
	}
	return string(h)[HashLength-hostHashLength:]
}
