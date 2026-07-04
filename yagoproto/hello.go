package yagoproto

import (
	"context"
	"fmt"
	"net/url"

	"github.com/D4rk4/yago/yagomodel"
)

type HelloRequest struct {
	NetworkName string
	Key         string
	Seed        yagomodel.Seed
	Count       int
	Iam         yagomodel.Hash
	MagicMD5    string
	MyTime      string
}

type HelloResponse struct {
	ResponseHeader
	YourIP   string
	YourType yagomodel.PeerType
	MyTime   string
	Message  string
	Seeds    []yagomodel.Seed
}

func (r HelloResponse) OwnSeed() yagomodel.Optional[yagomodel.Seed] {
	if len(r.Seeds) == 0 {
		return yagomodel.None[yagomodel.Seed]()
	}

	return yagomodel.Some(r.Seeds[0])
}

func (r HelloResponse) KnownSeeds() []yagomodel.Seed {
	if len(r.Seeds) < 2 {
		return nil
	}

	return r.Seeds[1:]
}

func (r HelloRequest) Form() url.Values {
	form := url.Values{}
	putString(form, FieldNetworkName, r.NetworkName)
	putString(form, FieldKey, r.Key)
	putString(form, FieldSeed, yagomodel.EncodeCompactWireForm(r.Seed.String()))
	putInt(form, FieldCount, r.Count)
	putString(form, FieldIam, r.Iam.String())
	putString(form, FieldMagicMD5, r.MagicMD5)
	putString(form, FieldMyTime, r.MyTime)

	return form
}

func ParseHelloRequest(ctx context.Context, form url.Values) (HelloRequest, error) {
	count, err := optionalInt(FieldCount, form.Get(FieldCount))
	if err != nil {
		return HelloRequest{}, err
	}

	req := HelloRequest{
		NetworkName: form.Get(FieldNetworkName),
		Key:         form.Get(FieldKey),
		Count:       count,
		MagicMD5:    form.Get(FieldMagicMD5),
		MyTime:      form.Get(FieldMyTime),
	}

	raw := form.Get(FieldSeed)
	if raw == "" {
		return HelloRequest{}, fmt.Errorf("hello request: missing %s", FieldSeed)
	}
	req.Seed, err = decodeSeed(ctx, raw)
	if err != nil {
		return HelloRequest{}, err
	}

	if raw := form.Get(FieldIam); raw != "" {
		req.Iam, err = yagomodel.ParseHash(raw)
		if err != nil {
			return HelloRequest{}, fmt.Errorf("hello request %s: %w", FieldIam, err)
		}
	}

	return req, nil
}

func (r HelloResponse) Encode() yagomodel.Message {
	msg := yagomodel.Message{}
	setString(msg, FieldYourIP, r.YourIP)
	setString(msg, FieldYourType, r.YourType.String())
	setString(msg, FieldMyTime, r.MyTime)
	setString(msg, FieldMessage, r.Message)
	for i, seed := range r.Seeds {
		setString(msg, indexedKey(prefixSeed, i), yagomodel.EncodeCompactWireForm(seed.String()))
	}

	return msg
}

func ParseHelloResponse(ctx context.Context, m yagomodel.Message) (HelloResponse, error) {
	header, err := parseResponseHeader(m)
	if err != nil {
		return HelloResponse{}, err
	}

	resp := HelloResponse{
		ResponseHeader: header,
		YourIP:         m[FieldYourIP],
		MyTime:         m[FieldMyTime],
		Message:        m[FieldMessage],
	}

	if raw := m[FieldYourType]; raw != "" {
		resp.YourType, err = yagomodel.ParsePeerType(raw)
		if err != nil {
			return HelloResponse{}, fmt.Errorf("hello response %s: %w", FieldYourType, err)
		}
	}

	resp.Seeds, err = decodeSeeds(ctx, m)
	if err != nil {
		return HelloResponse{}, err
	}

	return resp, nil
}

func decodeSeed(ctx context.Context, raw string) (yagomodel.Seed, error) {
	plain, err := yagomodel.DecodeWireForm(ctx, raw)
	if err != nil {
		return yagomodel.Seed{}, fmt.Errorf("seed wire form: %w", err)
	}

	seed, err := yagomodel.ParseSeed(ctx, plain)
	if err != nil {
		return yagomodel.Seed{}, fmt.Errorf("seed: %w", err)
	}

	return seed, nil
}

func decodeSeeds(ctx context.Context, m yagomodel.Message) ([]yagomodel.Seed, error) {
	var seeds []yagomodel.Seed
	for i := 0; ; i++ {
		raw, ok := m[indexedKey(prefixSeed, i)]
		if !ok {
			return seeds, nil
		}

		seed, err := decodeSeed(ctx, raw)
		if err != nil {
			return nil, err
		}

		seeds = append(seeds, seed)
	}
}
