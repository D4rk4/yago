package yagomodel

import (
	"fmt"
	"slices"
	"strings"
)

func EncodeSearchIndexAbstract(urlHashes []Hash) string {
	if len(urlHashes) == 0 {
		return "{}"
	}

	domains := make(map[string][]string)
	for _, hash := range urlHashes {
		if !hash.Valid() {
			continue
		}
		raw := hash.String()
		domains[raw[6:]] = append(domains[raw[6:]], raw[:6])
	}

	if len(domains) == 0 {
		return "{}"
	}

	keys := make([]string, 0, len(domains))
	for domain := range domains {
		keys = append(keys, domain)
	}
	slices.SortFunc(keys, compareBase64Strings)
	for _, domain := range keys {
		slices.SortFunc(domains[domain], compareBase64Strings)
	}

	var b strings.Builder
	b.WriteByte('{')
	for i, domain := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(domain)
		b.WriteByte(':')
		for _, path := range domains[domain] {
			b.WriteString(path)
		}
	}
	b.WriteByte('}')
	return b.String()
}

func DecodeSearchIndexAbstract(raw string) ([]Hash, error) {
	return DecodeSearchIndexAbstractWithLimit(raw, len(raw)/hostHashLength)
}

func DecodeSearchIndexAbstractWithLimit(raw string, maximumHashes int) ([]Hash, error) {
	if raw == "" || raw == "{}" {
		return nil, nil
	}
	if !strings.HasPrefix(raw, "{") || !strings.HasSuffix(raw, "}") {
		return nil, fmt.Errorf("%w: index abstract envelope", ErrInvalidHash)
	}

	body := raw[1 : len(raw)-1]
	maximumHashes = max(0, maximumHashes)
	var hashes []Hash
	if maximumHashes > 0 {
		hashes = make([]Hash, 0, min(maximumHashes, len(body)/HashLength))
	}
	for body != "" {
		group, remainder, more := strings.Cut(body, ",")
		var err error
		hashes, err = decodeSearchIndexAbstractGroup(group, hashes, maximumHashes)
		if err != nil {
			return nil, err
		}
		if more && remainder == "" {
			return nil, fmt.Errorf("%w: index abstract group %q", ErrInvalidHash, remainder)
		}
		body = remainder
	}

	return hashes, nil
}

func decodeSearchIndexAbstractGroup(
	group string,
	hashes []Hash,
	maximumHashes int,
) ([]Hash, error) {
	domain, paths, ok := strings.Cut(group, ":")
	if !ok || len(domain) != hostHashLength || len(paths)%hostHashLength != 0 {
		return nil, fmt.Errorf("%w: index abstract group %q", ErrInvalidHash, group)
	}
	if !validAbstractHashPart(domain) {
		return nil, fmt.Errorf("%w: index abstract host %q", ErrInvalidHash, domain)
	}
	for start := 0; start < len(paths); start += hostHashLength {
		path := paths[start : start+hostHashLength]
		if !validAbstractHashPart(path) {
			return nil, fmt.Errorf("%w: index abstract path", ErrInvalidHash)
		}
		if len(hashes) < maximumHashes {
			hashes = append(hashes, Hash(path+domain))
		}
	}

	return hashes, nil
}

func validAbstractHashPart(part string) bool {
	for position := range len(part) {
		if decodeTable[part[position]] < 0 {
			return false
		}
	}

	return true
}

func compareBase64Strings(a, b string) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		av := base64Order(a[i])
		bv := base64Order(b[i])
		if av < bv {
			return -1
		}
		if av > bv {
			return 1
		}
	}
	if len(a) < len(b) {
		return -1
	}
	if len(a) > len(b) {
		return 1
	}
	return 0
}

func base64Order(c byte) int {
	for i := range len(Alphabet) {
		if Alphabet[i] == c {
			return i
		}
	}
	return len(Alphabet) + int(c)
}
