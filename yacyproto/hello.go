package yacyproto

import (
	"context"
	"fmt"
	"net/url"

	"github.com/D4rk4/yago/yacymodel"
)

type HelloRequest struct {
	NetworkName string
	Key         string
	Seed        yacymodel.Seed
	Count       int
	Iam         yacymodel.Hash
	MagicMD5    string
	MyTime      string
}

type HelloResponse struct {
	ResponseHeader
	YourIP   string
	YourType yacymodel.PeerType
	MyTime   string
	Message  string
	Seeds    []yacymodel.Seed
}

func (r HelloResponse) OwnSeed() yacymodel.Optional[yacymodel.Seed] {
	if len(r.Seeds) == 0 {
		return yacymodel.None[yacymodel.Seed]()
	}

	return yacymodel.Some(r.Seeds[0])
}

func (r HelloResponse) KnownSeeds() []yacymodel.Seed {
	if len(r.Seeds) < 2 {
		return nil
	}

	return r.Seeds[1:]
}

func (r HelloRequest) Form() url.Values {
	form := url.Values{}
	putString(form, FieldNetworkName, r.NetworkName)
	putString(form, FieldKey, r.Key)
	putString(form, FieldSeed, yacymodel.EncodeCompactWireForm(r.Seed.String()))
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
		req.Iam, err = yacymodel.ParseHash(raw)
		if err != nil {
			return HelloRequest{}, fmt.Errorf("hello request %s: %w", FieldIam, err)
		}
	}

	return req, nil
}

func (r HelloResponse) Encode() yacymodel.Message {
	msg := yacymodel.Message{}
	setString(msg, FieldYourIP, r.YourIP)
	setString(msg, FieldYourType, r.YourType.String())
	setString(msg, FieldMyTime, r.MyTime)
	setString(msg, FieldMessage, r.Message)
	for i, seed := range r.Seeds {
		setString(msg, indexedKey(prefixSeed, i), yacymodel.EncodeCompactWireForm(seed.String()))
	}

	return msg
}

func ParseHelloResponse(ctx context.Context, m yacymodel.Message) (HelloResponse, error) {
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
		resp.YourType, err = yacymodel.ParsePeerType(raw)
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

func decodeSeed(ctx context.Context, raw string) (yacymodel.Seed, error) {
	plain, err := yacymodel.DecodeWireForm(ctx, raw)
	if err != nil {
		return yacymodel.Seed{}, fmt.Errorf("seed wire form: %w", err)
	}

	seed, err := yacymodel.ParseSeed(ctx, plain)
	if err != nil {
		return yacymodel.Seed{}, fmt.Errorf("seed: %w", err)
	}

	return seed, nil
}

func decodeSeeds(ctx context.Context, m yacymodel.Message) ([]yacymodel.Seed, error) {
	var seeds []yacymodel.Seed
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
