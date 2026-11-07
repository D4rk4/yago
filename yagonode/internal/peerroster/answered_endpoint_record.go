package peerroster

import (
	"crypto/sha256"
	"fmt"
	"net/url"
	"slices"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/peerwire"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const (
	answeredEndpointsBucket     vault.Name = "peerroster_answered_endpoints"
	answeredEndpointMagic                  = "YPE1"
	answeredEndpointHeaderWidth            = len(answeredEndpointMagic) + sha256.Size
)

type answeredEndpointRecord struct {
	advertisementIdentity [sha256.Size]byte
	endpoint              peerwire.Endpoint
}

type answeredEndpointCodec struct{}

func (answeredEndpointCodec) Encode(record answeredEndpointRecord) ([]byte, error) {
	origin := record.endpoint.Origin()
	raw := make([]byte, 0, answeredEndpointHeaderWidth+len(origin))
	raw = append(raw, answeredEndpointMagic...)
	raw = append(raw, record.advertisementIdentity[:]...)

	return append(raw, origin...), nil
}

func (answeredEndpointCodec) Decode(raw []byte) (answeredEndpointRecord, error) {
	if len(raw) < answeredEndpointHeaderWidth {
		return answeredEndpointRecord{}, fmt.Errorf(
			"decode answered endpoint: invalid record width",
		)
	}
	if string(raw[:len(answeredEndpointMagic)]) != answeredEndpointMagic {
		return answeredEndpointRecord{}, fmt.Errorf(
			"decode answered endpoint: unknown record format marker",
		)
	}
	endpoint, err := peerwire.ParseEndpointOrigin(string(raw[answeredEndpointHeaderWidth:]))
	if err != nil {
		return answeredEndpointRecord{}, fmt.Errorf("decode answered endpoint: %w", err)
	}
	record := answeredEndpointRecord{endpoint: endpoint}
	copy(
		record.advertisementIdentity[:],
		raw[len(answeredEndpointMagic):answeredEndpointHeaderWidth],
	)

	return record, nil
}

func answeredEndpointRecordFor(
	entry rosterEntry,
	endpoint peerwire.Endpoint,
) (answeredEndpointRecord, bool) {
	identity, candidates, ok := verifiedAdvertisement(entry)
	if !ok {
		return answeredEndpointRecord{}, false
	}
	advertised := slices.ContainsFunc(candidates, func(candidate *url.URL) bool {
		return peerwire.EndpointAt(candidate) == endpoint
	})
	if !advertised {
		return answeredEndpointRecord{}, false
	}

	return answeredEndpointRecord{advertisementIdentity: identity, endpoint: endpoint}, true
}

func (record answeredEndpointRecord) appliesTo(entry rosterEntry) bool {
	identity, _, ok := verifiedAdvertisement(entry)

	return ok && identity == record.advertisementIdentity
}

func verifiedAdvertisement(entry rosterEntry) ([sha256.Size]byte, []*url.URL, bool) {
	if !entry.verified {
		return [sha256.Size]byte{}, nil, false
	}
	candidates, err := peerwire.ContactEndpoints(
		entry.seed,
		"",
		true,
		yagomodel.None[peerwire.Endpoint](),
	)
	if err != nil {
		return [sha256.Size]byte{}, nil, false
	}
	origins := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		origins = append(origins, peerwire.EndpointAt(candidate).Origin())
	}
	slices.Sort(origins)
	identity := sha256.New()
	_, _ = identity.Write([]byte(entry.seed.Hash.String()))
	for _, origin := range origins {
		_, _ = identity.Write([]byte{0})
		_, _ = identity.Write([]byte(origin))
	}
	var sum [sha256.Size]byte
	copy(sum[:], identity.Sum(nil))

	return sum, candidates, true
}
