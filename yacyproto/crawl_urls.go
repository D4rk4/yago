package yacyproto

import (
	"context"
	"net/url"

	"github.com/D4rk4/yago/yacymodel"
)

type CrawlURLCall string

const (
	CrawlURLCallRemoteCrawl CrawlURLCall = "remotecrawl"
	CrawlURLCallURLHashList CrawlURLCall = "urlhashlist"
)

const (
	CrawlURLResponseRejected = "rejected - insufficient call parameters"
	CrawlURLResponseOK       = "ok"
)

type CrawlURLRequest struct {
	NetworkName string
	Iam         string
	YouAre      string
	Key         string
	MagicMD5    string
	MyTime      string
	Call        CrawlURLCall
	Count       yacymodel.Optional[int]
	Time        yacymodel.Optional[int]
	Hashes      string
}

func (r CrawlURLRequest) Form() url.Values {
	form := url.Values{}
	putString(form, FieldNetworkName, r.NetworkName)
	putString(form, FieldIam, r.Iam)
	putString(form, FieldYouAre, r.YouAre)
	putString(form, FieldKey, r.Key)
	putString(form, FieldMagicMD5, r.MagicMD5)
	putString(form, FieldMyTime, r.MyTime)
	putString(form, FieldCall, string(r.Call))
	if count, ok := r.Count.Get(); ok {
		putInt(form, FieldCount, count)
	}
	if timeout, ok := r.Time.Get(); ok {
		putInt(form, FieldTime, timeout)
	}
	putString(form, FieldHashes, r.Hashes)

	return form
}

func ParseCrawlURLRequest(_ context.Context, form url.Values) (CrawlURLRequest, error) {
	req := CrawlURLRequest{
		NetworkName: form.Get(FieldNetworkName),
		Iam:         form.Get(FieldIam),
		YouAre:      form.Get(FieldYouAre),
		Key:         form.Get(FieldKey),
		MagicMD5:    form.Get(FieldMagicMD5),
		MyTime:      form.Get(FieldMyTime),
		Call:        CrawlURLCall(form.Get(FieldCall)),
		Hashes:      form.Get(FieldHashes),
	}

	if raw := form.Get(FieldCount); raw != "" {
		count, err := readInt(FieldCount, raw)
		if err != nil {
			return CrawlURLRequest{}, err
		}
		req.Count = yacymodel.Some(count)
	}

	if raw := form.Get(FieldTime); raw != "" {
		timeout, err := readInt(FieldTime, raw)
		if err != nil {
			return CrawlURLRequest{}, err
		}
		req.Time = yacymodel.Some(timeout)
	}

	return req, nil
}

func (r CrawlURLRequest) HashList() ([]yacymodel.Hash, bool) {
	if len(r.Hashes)%yacymodel.HashLength != 0 {
		return nil, false
	}

	hashes := make([]yacymodel.Hash, 0, len(r.Hashes)/yacymodel.HashLength)
	for i := 0; i < len(r.Hashes); i += yacymodel.HashLength {
		hashes = append(hashes, yacymodel.Hash(r.Hashes[i:i+yacymodel.HashLength]))
	}

	return hashes, true
}
