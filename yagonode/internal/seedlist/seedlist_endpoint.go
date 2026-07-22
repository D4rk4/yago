package seedlist

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net"
	"slices"
	"strings"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/httpguard"
	"github.com/D4rk4/yago/yagoproto"
)

const (
	seedlistHTMLContentType = "text/html; charset=UTF-8"
	seedlistJSONContentType = "application/json; charset=UTF-8"
	seedlistXMLContentType  = "application/xml; charset=UTF-8"
	seedlistLineBreak       = "\r\n"
	seedlistMaxEntries      = yagoproto.SeedlistMaximumEntries
)

type endpoint struct {
	status RuntimeStatus
	peers  PeerDirectory
}

var marshalSeedlistJSON = json.MarshalIndent

func (e endpoint) ServeHTML(
	ctx context.Context,
	req yagoproto.SeedlistRequest,
) (httpguard.RawResponse, error) {
	return httpguard.RawResponse{
		ContentType: seedlistHTMLContentType,
		Body:        encodeSeeds(e.selectSeeds(ctx, req)),
	}, nil
}

func (e endpoint) ServeJSON(
	ctx context.Context,
	req yagoproto.SeedlistRequest,
) (httpguard.RawResponse, error) {
	seeds := e.selectSeeds(ctx, req)
	if !req.OwnSeedOnly {
		seeds = filterByName(seeds, req.PeerName, req.PeerNamePresent)
	}
	body, err := encodeJSON(seeds, req)
	if err != nil {
		return httpguard.RawResponse{}, err
	}

	return httpguard.RawResponse{ContentType: seedlistJSONContentType, Body: body}, nil
}

func (e endpoint) ServeXML(
	ctx context.Context,
	req yagoproto.SeedlistRequest,
) (httpguard.RawResponse, error) {
	return httpguard.RawResponse{
		ContentType: seedlistXMLContentType,
		Body:        encodeXML(e.xmlSeeds(ctx, req), req),
	}, nil
}

func filterByName(seeds []yagomodel.Seed, name string, present bool) []yagomodel.Seed {
	if name == "" && !present {
		return seeds
	}

	filtered := seeds[:0]
	for _, seed := range seeds {
		if seedNameMatches(seed, name) {
			filtered = append(filtered, seed)
		}
	}

	return filtered
}

func seedPassesVersionFloor(seed yagomodel.Seed, floor float64) bool {
	version, ok := seed.Version.Get()
	if !ok {
		return true
	}

	value, err := version.Float()

	return err != nil || value == 0 || value >= floor
}

func seedNameMatches(seed yagomodel.Seed, name string) bool {
	seedName, ok := seed.Name.Get()

	return ok && seedName == name
}

func encodeSeeds(seeds []yagomodel.Seed) string {
	var b strings.Builder
	for _, seed := range seeds {
		b.WriteString(yagomodel.EncodeSeedWireForm(seed))
		b.WriteString(seedlistLineBreak)
	}

	return b.String()
}

func encodeJSON(seeds []yagomodel.Seed, req yagoproto.SeedlistRequest) (string, error) {
	payload := map[string][]map[string]any{
		"peers": seedObjects(seeds, req),
	}
	encoded, err := marshalSeedlistJSON(payload, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode seedlist json: %w", err)
	}

	body := string(encoded)
	if req.CallbackPresent || req.Callback != "" {
		body = req.Callback + "([" + body + "]);"
	}

	return body, nil
}

func seedObjects(
	seeds []yagomodel.Seed,
	req yagoproto.SeedlistRequest,
) []map[string]any {
	objects := make([]map[string]any, 0, len(seeds))
	for _, seed := range seeds {
		addresses := seedAddresses(seed)
		if len(addresses) == 0 {
			continue
		}

		object := map[string]any{yagomodel.SeedHash: seed.Hash.String()}
		if !req.AddressOnly {
			props := seed.Properties()
			for _, key := range sortedSeedPropertyKeys(seed) {
				if key != yagomodel.SeedHash {
					object[key] = props[key]
				}
			}
		}
		object["Address"] = addresses
		objects = append(objects, object)
	}

	return objects
}

func encodeXML(seeds []yagomodel.Seed, req yagoproto.SeedlistRequest) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0"?>`)
	b.WriteByte('\n')
	b.WriteString("<peers>")
	b.WriteByte('\n')
	for _, seed := range seeds {
		addresses := seedAddresses(seed)
		if len(addresses) == 0 {
			continue
		}

		b.WriteString("<seed>")
		b.WriteByte('\n')
		writeXMLElement(&b, yagomodel.SeedHash, seed.Hash.String())
		if !req.AddressOnly {
			props := seed.Properties()
			for _, key := range sortedSeedPropertyKeys(seed) {
				if key != yagomodel.SeedHash {
					writeXMLElement(&b, key, props[key])
				}
			}
		}
		for _, address := range addresses {
			writeXMLElement(&b, "Address", address)
		}
		b.WriteString("</seed>")
		b.WriteByte('\n')
	}
	b.WriteString("</peers>")

	return b.String()
}

func writeXMLElement(b *strings.Builder, key, value string) {
	b.WriteByte('<')
	b.WriteString(key)
	b.WriteByte('>')
	_ = xml.EscapeText(b, []byte(value))
	b.WriteString("</")
	b.WriteString(key)
	b.WriteByte('>')
	b.WriteByte('\n')
}

func sortedSeedPropertyKeys(seed yagomodel.Seed) []string {
	props := seed.Properties()
	keys := make([]string, 0, len(props))
	for key := range props {
		keys = append(keys, key)
	}
	slices.Sort(keys)

	return keys
}

func seedAddresses(seed yagomodel.Seed) []string {
	port, ok := seed.Port.Get()
	if !ok {
		return nil
	}

	var addresses []string
	seen := make(map[string]struct{})
	appendAddress := func(host yagomodel.Host) {
		address := net.JoinHostPort(host.String(), port.String())
		if _, exists := seen[address]; exists {
			return
		}
		seen[address] = struct{}{}
		addresses = append(addresses, address)
	}
	if host, ok := seed.IP.Get(); ok {
		appendAddress(host)
	}
	if hosts, ok := seed.IP6.Get(); ok {
		for _, host := range hosts {
			appendAddress(host)
		}
	}

	return addresses
}
