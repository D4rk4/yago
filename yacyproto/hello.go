package yacyproto

import (
	"fmt"
	"net/url"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

// HelloRequest is the GET|POST /yacy/hello.html request: a peer handshake and
// seed exchange.
type HelloRequest struct {
	NetworkName string
	Key         string
	Seed        yacymodel.Seed
	Count       int
	Iam         yacymodel.Hash
	MagicMD5    string
	MyTime      string
}

// HelloResponse is the /yacy/hello.html response. Seeds[0] is always the
// responder's own seed; the rest are additional known seeds.
type HelloResponse struct {
	ResponseHeader
	YourIP   string
	YourType yacymodel.PeerType
	MyTime   string
	Message  string
	Seeds    []yacymodel.Seed
}

// Form renders the request as HTTP form fields.
func (r HelloRequest) Form() url.Values {
	form := url.Values{}
	putString(form, FieldNetworkName, r.NetworkName)
	putString(form, FieldKey, r.Key)
	if r.Seed != nil {
		putString(form, FieldSeed, yacymodel.EncodeSeedWireForm(r.Seed.String()))
	}
	putInt(form, FieldCount, r.Count)
	putString(form, FieldIam, r.Iam.String())
	putString(form, FieldMagicMD5, r.MagicMD5)
	putString(form, FieldMyTime, r.MyTime)

	return form
}

// ParseHelloRequest reads a HelloRequest from HTTP form fields.
func ParseHelloRequest(form url.Values) (HelloRequest, error) {
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

	if raw := form.Get(FieldSeed); raw != "" {
		req.Seed, err = decodeSeed(raw)
		if err != nil {
			return HelloRequest{}, err
		}
	}

	if raw := form.Get(FieldIam); raw != "" {
		req.Iam, err = yacymodel.ParseHash(raw)
		if err != nil {
			return HelloRequest{}, fmt.Errorf("hello request %s: %w", FieldIam, err)
		}
	}

	return req, nil
}

// Encode renders the response as a key=value message.
func (r HelloResponse) Encode() yacymodel.Message {
	msg := yacymodel.Message{}
	r.write(msg)
	setString(msg, FieldYourIP, r.YourIP)
	setString(msg, FieldYourType, r.YourType.String())
	setString(msg, FieldMyTime, r.MyTime)
	setString(msg, FieldMessage, r.Message)
	for i, seed := range r.Seeds {
		setString(msg, indexedKey(prefixSeed, i), yacymodel.EncodeSeedWireForm(seed.String()))
	}

	return msg
}

// ParseHelloResponse reads a HelloResponse from key=value lines.
func ParseHelloResponse(m yacymodel.Message) (HelloResponse, error) {
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

	resp.Seeds, err = decodeSeeds(m)
	if err != nil {
		return HelloResponse{}, err
	}

	return resp, nil
}

func decodeSeed(raw string) (yacymodel.Seed, error) {
	plain, err := yacymodel.DecodeSeedWireForm(raw)
	if err != nil {
		return nil, fmt.Errorf("seed wire form: %w", err)
	}

	seed, err := yacymodel.ParseSeed(plain)
	if err != nil {
		return nil, fmt.Errorf("seed: %w", err)
	}

	return seed, nil
}

func decodeSeeds(m yacymodel.Message) ([]yacymodel.Seed, error) {
	var seeds []yacymodel.Seed
	for i := 0; ; i++ {
		raw, ok := m[indexedKey(prefixSeed, i)]
		if !ok {
			return seeds, nil
		}

		seed, err := decodeSeed(raw)
		if err != nil {
			return nil, err
		}

		seeds = append(seeds, seed)
	}
}
