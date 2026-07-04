package yagomodel

import (
	"fmt"
	"net/netip"
	"strings"
)

func ParseIP6(value string) ([]Host, error) {
	hosts := []Host{}
	for segment := range strings.SplitSeq(value, "|") {
		addr, err := netip.ParseAddr(segment)
		if err != nil {
			return nil, fmt.Errorf("%w: %q", ErrBadHost, value)
		}
		hosts = append(hosts, Host{addr: addr})
	}

	return hosts, nil
}
