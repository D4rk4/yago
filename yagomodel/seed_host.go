package yagomodel

import (
	"errors"
	"fmt"
	"net/netip"
	"strings"
)

const hostLabelMaxLength = 63

var ErrBadHost = errors.New("bad host")

type Host struct {
	addr     netip.Addr
	hostname string
}

func ParseHost(s string) (Host, error) {
	if addr, err := netip.ParseAddr(s); err == nil {
		return Host{addr: addr}, nil
	}
	if isHostname(s) {
		return Host{hostname: s}, nil
	}
	return Host{}, fmt.Errorf("%w: %q", ErrBadHost, s)
}

func (h Host) String() string {
	if h.addr.IsValid() {
		return h.addr.String()
	}
	return h.hostname
}

func isHostname(s string) bool {
	if s == "" || len(s) > 255 {
		return false
	}
	for _, label := range strings.Split(s, ".") {
		if !isHostLabel(label) {
			return false
		}
	}
	return true
}

func isHostLabel(label string) bool {
	if label == "" || len(label) > hostLabelMaxLength {
		return false
	}
	if label[0] == '-' || label[len(label)-1] == '-' {
		return false
	}
	for _, r := range label {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' {
			continue
		}
		return false
	}
	return true
}
