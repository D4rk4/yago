package yacymodel

import (
	"errors"
	"fmt"
	"strconv"
)

const (
	portMin = 1
	portMax = 65535
)

var ErrBadPort = errors.New("bad port")

type Port int

func ParsePort(s string) (Port, error) {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("%w: %w", ErrBadPort, err)
	}
	if n < portMin || n > portMax {
		return 0, fmt.Errorf("%w: %d out of range", ErrBadPort, n)
	}
	return Port(n), nil
}

func (p Port) String() string {
	return strconv.Itoa(int(p))
}
