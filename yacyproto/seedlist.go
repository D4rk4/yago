package yacyproto

import (
	"context"
	"net/url"
	"strconv"

	"github.com/D4rk4/yago/yacymodel"
)

type SeedlistRequest struct {
	MaxCount    yacymodel.Optional[int]
	MinVersion  yacymodel.Optional[float64]
	NodeOnly    bool
	IncludeSelf bool
	OwnSeedOnly bool
	ID          yacymodel.Optional[yacymodel.Hash]
	Name        string
	AddressOnly bool
	Callback    string
	PeerName    string
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
	}
	putString(form, FieldSeedlistName, r.Name)
	if r.AddressOnly {
		form.Set(FieldSeedlistAddress, strconv.FormatBool(true))
	}
	putString(form, FieldSeedlistCallback, r.Callback)
	putString(form, FieldSeedlistPeerName, r.PeerName)

	return form
}

func ParseSeedlistRequest(_ context.Context, form url.Values) (SeedlistRequest, error) {
	req := SeedlistRequest{IncludeSelf: true}

	if raw := form.Get(FieldSeedlistMaxCount); raw != "" {
		maxCount, err := readInt(FieldSeedlistMaxCount, raw)
		if err != nil {
			return SeedlistRequest{}, err
		}
		req.MaxCount = yacymodel.Some(maxCount)
	}
	if raw := form.Get(FieldSeedlistMinVersion); raw != "" {
		minVersion, err := readFloat(FieldSeedlistMinVersion, raw)
		if err != nil {
			return SeedlistRequest{}, err
		}
		req.MinVersion = yacymodel.Some(minVersion)
	}

	nodeOnly, err := seedlistBool(FieldSeedlistNode, form.Get(FieldSeedlistNode), false)
	if err != nil {
		return SeedlistRequest{}, err
	}
	req.NodeOnly = nodeOnly

	includeSelf, err := seedlistBool(FieldSeedlistMe, form.Get(FieldSeedlistMe), true)
	if err != nil {
		return SeedlistRequest{}, err
	}
	req.IncludeSelf = includeSelf

	ownSeedOnly, err := seedlistBool(FieldSeedlistMy, form.Get(FieldSeedlistMy), false)
	if err != nil {
		return SeedlistRequest{}, err
	}
	req.OwnSeedOnly = ownSeedOnly

	if raw := form.Get(FieldSeedlistID); raw != "" {
		id, err := parseHashField("seedlist request", FieldSeedlistID, raw)
		if err != nil {
			return SeedlistRequest{}, err
		}
		req.ID = yacymodel.Some(id)
	}

	req.Name = form.Get(FieldSeedlistName)
	addressOnly, err := seedlistBool(FieldSeedlistAddress, form.Get(FieldSeedlistAddress), false)
	if err != nil {
		return SeedlistRequest{}, err
	}
	req.AddressOnly = addressOnly

	req.Callback = form.Get(FieldSeedlistCallback)
	req.PeerName = form.Get(FieldSeedlistPeerName)

	return req, nil
}

func seedlistBool(key, value string, fallback bool) (bool, error) {
	if value == "" {
		return fallback, nil
	}

	return optionalBool(key, value)
}
