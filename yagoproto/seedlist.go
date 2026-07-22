package yagoproto

import (
	"context"
	"net/url"
	"strconv"
	"strings"

	"github.com/D4rk4/yago/yagomodel"
)

const SeedlistMaximumEntries = 1000

type SeedlistRequest struct {
	MaxCount        yagomodel.Optional[int]
	MinVersion      yagomodel.Optional[float64]
	NodeOnly        bool
	IncludeSelf     bool
	OwnSeedOnly     bool
	ID              yagomodel.Optional[yagomodel.Hash]
	IDPresent       bool
	Name            string
	NamePresent     bool
	AddressOnly     bool
	Callback        string
	CallbackPresent bool
	PeerName        string
	PeerNamePresent bool
}

func (r SeedlistRequest) Form() url.Values {
	form := url.Values{}
	if maxCount, ok := r.MaxCount.Get(); ok {
		putInt(form, FieldSeedlistMaxCount, maxCount)
	}
	if minVersion, ok := r.MinVersion.Get(); ok {
		putFloat(form, FieldSeedlistMinVersion, minVersion)
	}
	if r.NodeOnly {
		form.Set(FieldSeedlistNode, strconv.FormatBool(true))
	}
	if !r.IncludeSelf {
		form.Set(FieldSeedlistMe, strconv.FormatBool(false))
	}
	if r.OwnSeedOnly {
		form.Set(FieldSeedlistMy, strconv.FormatBool(true))
	}
	if id, ok := r.ID.Get(); ok {
		form.Set(FieldSeedlistID, id.String())
	} else if r.IDPresent {
		form.Set(FieldSeedlistID, "")
	}
	if r.Name != "" || r.NamePresent {
		form.Set(FieldSeedlistName, r.Name)
	}
	if r.AddressOnly {
		form.Set(FieldSeedlistAddress, strconv.FormatBool(true))
	}
	if r.Callback != "" || r.CallbackPresent {
		form.Set(FieldSeedlistCallback, r.Callback)
	}
	if r.PeerName != "" || r.PeerNamePresent {
		form.Set(FieldSeedlistPeerName, r.PeerName)
	}

	return form
}

func ParseSeedlistRequest(_ context.Context, form url.Values) (SeedlistRequest, error) {
	req := SeedlistRequest{
		MinVersion:  yagomodel.Some(0.0),
		IncludeSelf: true,
	}

	if raw := form.Get(FieldSeedlistMaxCount); raw != "" {
		if maxCount, valid := parseJavaSignedDecimalInt32(raw); valid {
			req.MaxCount = yagomodel.Some(min(maxCount, SeedlistMaximumEntries))
		}
	}
	if raw := form.Get(FieldSeedlistMinVersion); raw != "" {
		if minVersion, parsed := parseSeedlistVersion(raw); parsed {
			req.MinVersion = yagomodel.Some(min(minVersion, float64(SeedlistMaximumEntries)))
		}
	}

	req.NodeOnly = seedlistBoolean(form, FieldSeedlistNode, false)
	req.IncludeSelf = seedlistBoolean(form, FieldSeedlistMe, true)

	// YaCy's seedlist servlet checks only for the key's presence
	// (post.containsKey("my")), never its value, so a bare "?my" — and even
	// "my=false" — selects the own seed. Mirror that for wire parity.
	req.OwnSeedOnly = form.Has(FieldSeedlistMy)

	req.IDPresent = form.Has(FieldSeedlistID)
	if raw := form.Get(FieldSeedlistID); req.IDPresent && raw != "" {
		id, err := parseHashField("seedlist request", FieldSeedlistID, raw)
		if err == nil {
			req.ID = yagomodel.Some(id)
		}
	}

	req.NamePresent = form.Has(FieldSeedlistName)
	req.Name = form.Get(FieldSeedlistName)
	req.AddressOnly = seedlistBoolean(form, FieldSeedlistAddress, false)

	req.CallbackPresent = form.Has(FieldSeedlistCallback)
	req.Callback = form.Get(FieldSeedlistCallback)
	req.PeerNamePresent = form.Has(FieldSeedlistPeerName)
	req.PeerName = form.Get(FieldSeedlistPeerName)

	return req, nil
}

func seedlistBoolean(form url.Values, key string, fallback bool) bool {
	if !form.Has(key) {
		return fallback
	}

	value := strings.ToLower(form.Get(key))

	return value == "true" || value == "on" || value == "1"
}
