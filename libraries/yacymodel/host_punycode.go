package yacymodel

import (
	"math"
	"strings"
)

const (
	punyBase        = 36
	punyTMin        = 1
	punyTMax        = 26
	punySkew        = 38
	punyDamp        = 700
	punyInitialBias = 72
	punyInitialN    = 128
	punyPrefix      = "xn--"
)

// In-house RFC 3492 encoder rather than x/net/idna: YaCy applies raw per-label
// punycode with no UTS-46 nameprep, and the URL hash must match it byte-for-byte.
func toPunycode(host string) string {
	if isBasicString(host) {
		return host
	}
	labels := strings.Split(host, ".")
	for i, label := range labels {
		if !isBasicString(label) {
			labels[i] = punyPrefix + punycodeEncode([]rune(label))
		}
	}
	return strings.Join(labels, ".")
}

func isBasicString(s string) bool {
	for _, r := range s {
		if r >= 0x80 {
			return false
		}
	}
	return true
}

//nolint:gocognit,revive // FIXME: split RFC 3492 encode steps after the new lint rules are committed.
func punycodeEncode(input []rune) string {
	n := punyInitialN
	delta := 0
	bias := punyInitialBias

	var out []byte
	for _, r := range input {
		if r < 0x80 {
			out = append(out, byte(r&0x7f))
		}
	}
	basic := len(out)
	handled := basic
	if basic > 0 {
		out = append(out, '-')
	}

	for handled < len(input) {
		m := math.MaxInt32
		for _, r := range input {
			if int(r) >= n && int(r) < m {
				m = int(r)
			}
		}
		delta += (m - n) * (handled + 1)
		n = m
		for _, r := range input {
			c := int(r)
			if c < n {
				delta++
			}
			if c == n {
				q := delta
				for k := punyBase; ; k += punyBase {
					t := punyThreshold(k, bias)
					if q < t {
						break
					}
					out = append(out, punyDigit(t+(q-t)%(punyBase-t)))
					q = (q - t) / (punyBase - t)
				}
				out = append(out, punyDigit(q))
				bias = punyAdapt(delta, handled+1, handled == basic)
				delta = 0
				handled++
			}
		}
		delta++
		n++
	}
	return string(out)
}

func punyThreshold(k, bias int) int {
	switch {
	case k <= bias:
		return punyTMin
	case k >= bias+punyTMax:
		return punyTMax
	default:
		return k - bias
	}
}

func punyAdapt(delta, numPoints int, firstTime bool) int {
	if firstTime {
		delta /= punyDamp
	} else {
		delta /= 2
	}
	delta += delta / numPoints
	k := 0
	for delta > ((punyBase-punyTMin)*punyTMax)/2 {
		delta /= punyBase - punyTMin
		k += punyBase
	}
	return k + (punyBase-punyTMin+1)*delta/(delta+punySkew)
}

func punyDigit(d int) byte {
	if d < 26 {
		return byte((d + 'a') & 0xff)
	}
	return byte((d - 26 + '0') & 0xff)
}
