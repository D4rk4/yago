package yacymodel

const MaxPosition = uint64(1)<<63 - 1

func Position(h Hash) (uint64, error) {
	if _, err := ParseHash(string(h)); err != nil {
		return 0, err
	}

	return cardinal(string(h))
}

func Distance(from, to uint64) uint64 {
	if to >= from {
		return to - from
	}
	return (MaxPosition - from) + to + 1
}
