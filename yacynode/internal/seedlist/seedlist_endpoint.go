package seedlist

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net"
	"slices"
	"strconv"
	"strings"

	"github.com/D4rk4/yago/yacymodel"
	"github.com/D4rk4/yago/yacynode/internal/httpguard"
	"github.com/D4rk4/yago/yacyproto"
)

const (
	seedlistHTMLContentType       = "text/plain; charset=UTF-8"
	seedlistJSONContentType       = "application/json; charset=UTF-8"
	seedlistJavaScriptContentType = "application/javascript; charset=UTF-8"
	seedlistXMLContentType        = "application/xml; charset=UTF-8"
	seedlistLineBreak             = "\r\n"
	seedlistMaxEntries            = 1000
)

type endpoint struct {
	status       RuntimeStatus
	reachability ReachablePeers
}

var marshalSeedlistJSON = json.MarshalIndent

func (e endpoint) ServeHTML(
	ctx context.Context,
	req yacyproto.SeedlistRequest,
) (httpguard.RawResponse, error) {
	return httpguard.RawResponse{
		ContentType: seedlistHTMLContentType,
		Body:        encodeSeeds(e.seeds(ctx, req)),
	}, nil
}

func (e endpoint) ServeJSON(
	ctx context.Context,
	req yacyproto.SeedlistRequest,
) (httpguard.RawResponse, error) {
	body, err := encodeJSON(e.seeds(ctx, req), req)
	if err != nil {
		return httpguard.RawResponse{}, err
	}

	contentType := seedlistJSONContentType
	if req.Callback != "" {
		contentType = seedlistJavaScriptContentType
	}

	return httpguard.RawResponse{ContentType: contentType, Body: body}, nil
}

func (e endpoint) ServeXML(
	ctx context.Context,
	req yacyproto.SeedlistRequest,
) (httpguard.RawResponse, error) {
	return httpguard.RawResponse{
		ContentType: seedlistXMLContentType,
		Body:        encodeXML(e.seeds(ctx, req), req),
	}, nil
}

func (e endpoint) seeds(ctx context.Context, req yacyproto.SeedlistRequest) []yacymodel.Seed {
	candidates := e.candidates(ctx, req)
	if req.NodeOnly {
		candidates = filterAddressed(candidates)
	}
	candidates = filterByID(candidates, req.ID)
	candidates = filterByName(candidates, req.Name)
	candidates = filterByName(candidates, req.PeerName)
	candidates = filterByMinVersion(candidates, req.MinVersion)

	return limitSeeds(candidates, req.MaxCount)
}

func (e endpoint) candidates(
	ctx context.Context,
	req yacyproto.SeedlistRequest,
) []yacymodel.Seed {
	if req.OwnSeedOnly {
		return []yacymodel.Seed{e.status.SelfSeed(ctx)}
	}

	reachable := e.reachability.ReachablePeers(ctx)
	seeds := make([]yacymodel.Seed, 0, 1+len(reachable))
	if req.IncludeSelf {
		seeds = append(seeds, e.status.SelfSeed(ctx))
	}

	return append(seeds, reachable...)
}

func filterByID(
	seeds []yacymodel.Seed,
	id yacymodel.Optional[yacymodel.Hash],
) []yacymodel.Seed {
	target, ok := id.Get()
	if !ok {
		return seeds
	}

	filtered := seeds[:0]
	for _, seed := range seeds {
		if seed.Hash == target {
			filtered = append(filtered, seed)
		}
	}

	return filtered
}

func filterByName(seeds []yacymodel.Seed, name string) []yacymodel.Seed {
	if name == "" {
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

func filterAddressed(seeds []yacymodel.Seed) []yacymodel.Seed {
	filtered := seeds[:0]
	for _, seed := range seeds {
		if len(seedAddresses(seed)) > 0 {
			filtered = append(filtered, seed)
		}
	}

	return filtered
}

func filterByMinVersion(
	seeds []yacymodel.Seed,
	minVersion yacymodel.Optional[float64],
) []yacymodel.Seed {
	floor, ok := minVersion.Get()
	if !ok || floor <= 0 {
		return seeds
	}

	filtered := seeds[:0]
	for _, seed := range seeds {
		if seedVersionAtLeast(seed, floor) {
			filtered = append(filtered, seed)
		}
	}

	return filtered
}

func seedVersionAtLeast(seed yacymodel.Seed, floor float64) bool {
	version, ok := seed.Version.Get()
	if !ok {
		return false
	}

	value, err := strconv.ParseFloat(version.String(), 64)

	return err == nil && value >= floor
}

func seedNameMatches(seed yacymodel.Seed, name string) bool {
	seedName, ok := seed.Name.Get()

	return ok && seedName == name
}

func limitSeeds(
	seeds []yacymodel.Seed,
	maxCount yacymodel.Optional[int],
) []yacymodel.Seed {
	limit := seedlistMaxEntries
	if requested, ok := maxCount.Get(); ok {
		limit = requested
	}
	if limit < 0 {
		limit = 0
	}
	if limit > seedlistMaxEntries {
		limit = seedlistMaxEntries
	}
	if limit < len(seeds) {
		return seeds[:limit]
	}

	return seeds
}

func encodeSeeds(seeds []yacymodel.Seed) string {
	var b strings.Builder
	for _, seed := range seeds {
		b.WriteString(seed.String())
		b.WriteString(seedlistLineBreak)
	}

	return b.String()
}

func encodeJSON(seeds []yacymodel.Seed, req yacyproto.SeedlistRequest) (string, error) {
	payload := map[string][]map[string]any{
		"peers": seedObjects(seeds, req),
	}
	encoded, err := marshalSeedlistJSON(payload, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode seedlist json: %w", err)
	}

	body := string(encoded)
	if req.Callback != "" {
		body = req.Callback + "([" + body + "]);"
	}

	return body, nil
}

func seedObjects(
	seeds []yacymodel.Seed,
	req yacyproto.SeedlistRequest,
) []map[string]any {
	objects := make([]map[string]any, 0, len(seeds))
	for _, seed := range seeds {
		addresses := seedAddresses(seed)
		if len(addresses) == 0 {
			continue
		}

		object := map[string]any{yacymodel.SeedHash: seed.Hash.String()}
		if !req.AddressOnly {
			props := seed.Properties()
			for _, key := range sortedSeedPropertyKeys(seed) {
				if key != yacymodel.SeedHash {
					object[key] = props[key]
				}
			}
		}
		object["Address"] = addresses
		objects = append(objects, object)
	}

	return objects
}

func encodeXML(seeds []yacymodel.Seed, req yacyproto.SeedlistRequest) string {
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
		writeXMLElement(&b, yacymodel.SeedHash, seed.Hash.String())
		if !req.AddressOnly {
			props := seed.Properties()
			for _, key := range sortedSeedPropertyKeys(seed) {
				if key != yacymodel.SeedHash {
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

func sortedSeedPropertyKeys(seed yacymodel.Seed) []string {
	props := seed.Properties()
	keys := make([]string, 0, len(props))
	for key := range props {
		keys = append(keys, key)
	}
	slices.Sort(keys)

	return keys
}

func seedAddresses(seed yacymodel.Seed) []string {
	port, ok := seed.Port.Get()
	if !ok {
		return nil
	}

	var addresses []string
	if host, ok := seed.IP.Get(); ok {
		addresses = append(addresses, net.JoinHostPort(host.String(), port.String()))
	}
	if hosts, ok := seed.IP6.Get(); ok {
		for _, host := range hosts {
			addresses = append(addresses, net.JoinHostPort(host.String(), port.String()))
		}
	}

	return addresses
}
