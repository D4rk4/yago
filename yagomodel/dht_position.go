package yagomodel

import (
	"errors"
	"fmt"
)

const MaxPosition = uint64(1)<<63 - 1

var ErrInvalidDHTPartition = errors.New("invalid dht partition")

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

func VerticalPartition(hash Hash, exponent int) (uint64, error) {
	position, err := Position(hash)
	if err != nil {
		return 0, err
	}

	shiftLength, err := verticalShiftLength(exponent)
	if err != nil {
		return 0, err
	}

	return position >> shiftLength, nil
}

func VerticalPosition(word Hash, vertical uint64, exponent int) (uint64, error) {
	wordPosition, err := Position(word)
	if err != nil {
		return 0, err
	}

	shiftLength, err := verticalShiftLength(exponent)
	if err != nil {
		return 0, err
	}
	if vertical >= uint64(1)<<exponent {
		return 0, fmt.Errorf(
			"%w: vertical %d outside exponent %d",
			ErrInvalidDHTPartition,
			vertical,
			exponent,
		)
	}

	partitionMask := (uint64(1) << shiftLength) - 1
	return (wordPosition & partitionMask) | (vertical << shiftLength), nil
}

func verticalShiftLength(exponent int) (int, error) {
	if exponent < 0 || exponent > 62 {
		return 0, fmt.Errorf("%w: exponent %d", ErrInvalidDHTPartition, exponent)
	}

	return 63 - exponent, nil
}
