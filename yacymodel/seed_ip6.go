package yacymodel

import (
	"fmt"
	"net/netip"
	"strings"
)

func ParseIP6(value string) (Host, error) {
	for segment := range strings.SplitSeq(value, "|") {
		addr, err := netip.ParseAddr(segment)
		if err != nil || !addr.Is6() {
			return "", fmt.Errorf("%w: %q", ErrBadHost, value)
		}
	}

	return Host(value), nil
}
