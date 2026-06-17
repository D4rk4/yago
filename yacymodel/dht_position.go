package yacymodel

// MaxPosition is the largest DHT ring position, the cardinal of a hash whose
// folded symbols are all the last alphabet entry.
const MaxPosition = uint64(1)<<63 - 1

// Position returns the DHT ring position of h: the enhanced-Base64 cardinal of
// the hash. It returns ErrInvalidBase64 for symbols outside the alphabet.
func Position(h Hash) (uint64, error) {
	return cardinal(string(h))
}

// Distance returns the forward ring distance from one position to another,
// wrapping at MaxPosition.
func Distance(from, to uint64) uint64 {
	if to >= from {
		return to - from
	}
	return (MaxPosition - from) + to + 1
}
