package yagoproto

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	"github.com/D4rk4/yago/yagomodel"
)

type TransferRWIRequest struct {
	NetworkName        string
	NetworkNamePresent bool
	Iam                yagomodel.Hash
	YouAre             yagomodel.Hash
	WordCount          int
	EntryCount         int
	Indexes            []yagomodel.RWIPosting
	Key                string
	MagicMD5           string

	wordCountPresent  bool
	entryCountPresent bool
	indexesPresent    bool
	indexesOverflow   bool
}

type TransferRWIResponse struct {
	ResponseHeader
	Result                 TransferRWIResult
	Pause                  int
	UnknownURL             []yagomodel.Hash
	ErrorURL               []yagomodel.Hash
	UnknownURLFieldPresent bool
}

func (r TransferRWIRequest) Form() url.Values {
	form := url.Values{}
	putNetworkName(form, r.NetworkName, r.NetworkNamePresent)
	putString(form, FieldIam, r.Iam.String())
	putString(form, FieldYouAre, r.YouAre.String())
	putInt(form, FieldWordCount, r.WordCount)
	putInt(form, FieldEntryCount, r.EntryCount)
	form.Set(FieldIndexes, encodeRWILines(r.Indexes))
	putString(form, FieldKey, r.Key)
	putString(form, FieldMagicMD5, r.MagicMD5)

	return form
}

func ParseTransferRWIRequest(ctx context.Context, form url.Values) (TransferRWIRequest, error) {
	wordCount, err := optionalInt(FieldWordCount, form.Get(FieldWordCount))
	if err != nil {
		return TransferRWIRequest{}, err
	}

	entryCount, err := optionalInt(FieldEntryCount, form.Get(FieldEntryCount))
	if err != nil {
		return TransferRWIRequest{}, err
	}

	networkName, networkNamePresent := parseNetworkName(form)
	req := TransferRWIRequest{
		NetworkName:        networkName,
		NetworkNamePresent: networkNamePresent,
		WordCount:          wordCount,
		EntryCount:         entryCount,
		Key:                form.Get(FieldKey),
		MagicMD5:           form.Get(FieldMagicMD5),
	}
	_, req.wordCountPresent = form[FieldWordCount]
	_, req.entryCountPresent = form[FieldEntryCount]
	_, req.indexesPresent = form[FieldIndexes]

	req.Iam, err = parseHashField("transferRWI request", FieldIam, form.Get(FieldIam))
	if err != nil {
		return TransferRWIRequest{}, err
	}

	req.YouAre, err = parseHashField("transferRWI request", FieldYouAre, form.Get(FieldYouAre))
	if err != nil {
		return TransferRWIRequest{}, err
	}

	req.Indexes, req.indexesOverflow = parseRWILines(ctx, form.Get(FieldIndexes))

	return req, nil
}

func (r TransferRWIRequest) MissingWordCountField() bool {
	return !r.wordCountPresent && r.WordCount == 0
}

func (r TransferRWIRequest) MissingEntryCountField() bool {
	return !r.entryCountPresent && r.EntryCount == 0
}

func (r TransferRWIRequest) MissingIndexesField() bool {
	return !r.indexesPresent && len(r.Indexes) == 0
}

func (r TransferRWIRequest) ExceedsEntryLimit() bool {
	return r.indexesOverflow ||
		r.EntryCount > MaximumTransferEntries ||
		len(r.Indexes) > MaximumTransferEntries
}

func (r TransferRWIResponse) Encode() yagomodel.Message {
	msg := yagomodel.Message{}
	setString(msg, FieldResult, string(r.Result))
	setInt(msg, FieldPause, r.Pause)
	msg[FieldUnknownURL] = joinHashes(r.UnknownURL)
	msg[FieldErrorURL] = joinHashes(r.ErrorURL)

	return msg
}

func ParseTransferRWIResponse(m yagomodel.Message) (TransferRWIResponse, error) {
	header, err := parseResponseHeader(m)
	if err != nil {
		return TransferRWIResponse{}, err
	}

	pause, err := optionalInt(FieldPause, m[FieldPause])
	if err != nil {
		return TransferRWIResponse{}, err
	}

	result, err := parseTransferRWIResult(m[FieldResult])
	if err != nil {
		return TransferRWIResponse{}, err
	}

	unknownRaw, unknownURLFieldPresent := m[FieldUnknownURL]
	if result == TransferRWIResult(ResultOK) && !unknownURLFieldPresent {
		return TransferRWIResponse{}, fmt.Errorf(
			"%w: transferRWI response missing %s",
			ErrBadField,
			FieldUnknownURL,
		)
	}
	unknown, err := splitHashes("transferRWI response", FieldUnknownURL, unknownRaw)
	if err != nil {
		return TransferRWIResponse{}, err
	}

	errorURL, err := splitHashes("transferRWI response", FieldErrorURL, m[FieldErrorURL])
	if err != nil {
		return TransferRWIResponse{}, err
	}

	return TransferRWIResponse{
		ResponseHeader:         header,
		Result:                 result,
		Pause:                  pause,
		UnknownURL:             unknown,
		ErrorURL:               errorURL,
		UnknownURLFieldPresent: unknownURLFieldPresent,
	}, nil
}

func encodeRWILines(entries []yagomodel.RWIPosting) string {
	lines := make([]string, len(entries))
	for i, entry := range entries {
		lines[i] = entry.String()
	}

	return strings.Join(lines, "\n")
}

func parseRWILines(ctx context.Context, raw string) ([]yagomodel.RWIPosting, bool) {
	if raw == "" {
		return nil, false
	}

	var entries []yagomodel.RWIPosting
	lines := 0
	for line := range strings.SplitSeq(raw, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		lines++
		if lines > MaximumTransferEntries {
			slog.WarnContext(
				ctx,
				"transfer rwi posting limit reached",
				slog.Int("limit", MaximumTransferEntries),
			)

			return entries, true
		}

		entry, err := yagomodel.ParseRWIPosting(line)
		if err != nil {
			slog.WarnContext(
				ctx,
				"transfer rwi posting discarded",
				slog.Any("error", err),
				slog.Int("lineLength", len(line)),
			)
			continue
		}

		entries = append(entries, entry)
	}

	return entries, false
}
