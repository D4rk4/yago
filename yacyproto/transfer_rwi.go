package yacyproto

import (
	"context"
	"log/slog"
	"net/url"
	"strings"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

type TransferRWIRequest struct {
	NetworkName string
	Iam         yacymodel.Hash
	YouAre      yacymodel.Hash
	WordCount   int
	EntryCount  int
	Indexes     []yacymodel.RWIPosting
	Key         string
}

type TransferRWIResponse struct {
	ResponseHeader
	Result     TransferRWIResult
	Pause      int
	UnknownURL []yacymodel.Hash
	ErrorURL   []yacymodel.Hash
}

func (r TransferRWIRequest) Form() url.Values {
	form := url.Values{}
	putString(form, FieldNetworkName, r.NetworkName)
	putString(form, FieldIam, r.Iam.String())
	putString(form, FieldYouAre, r.YouAre.String())
	putInt(form, FieldWordCount, r.WordCount)
	putInt(form, FieldEntryCount, r.EntryCount)
	putString(form, FieldIndexes, encodeRWILines(r.Indexes))
	putString(form, FieldKey, r.Key)

	return form
}

func ParseTransferRWIRequest(form url.Values) (TransferRWIRequest, error) {
	wordCount, err := optionalInt(FieldWordCount, form.Get(FieldWordCount))
	if err != nil {
		return TransferRWIRequest{}, err
	}

	entryCount, err := optionalInt(FieldEntryCount, form.Get(FieldEntryCount))
	if err != nil {
		return TransferRWIRequest{}, err
	}

	req := TransferRWIRequest{
		NetworkName: form.Get(FieldNetworkName),
		WordCount:   wordCount,
		EntryCount:  entryCount,
		Key:         form.Get(FieldKey),
	}

	req.Iam, err = parseHashField("transferRWI request", FieldIam, form.Get(FieldIam))
	if err != nil {
		return TransferRWIRequest{}, err
	}

	req.YouAre, err = parseHashField("transferRWI request", FieldYouAre, form.Get(FieldYouAre))
	if err != nil {
		return TransferRWIRequest{}, err
	}

	req.Indexes = parseRWILines(form.Get(FieldIndexes))

	return req, nil
}

func (r TransferRWIResponse) Encode() yacymodel.Message {
	msg := yacymodel.Message{}
	r.write(msg)
	setString(msg, FieldResult, string(r.Result))
	setInt(msg, FieldPause, r.Pause)
	msg[FieldUnknownURL] = joinHashes(r.UnknownURL)
	msg[FieldErrorURL] = joinHashes(r.ErrorURL)

	return msg
}

func ParseTransferRWIResponse(m yacymodel.Message) (TransferRWIResponse, error) {
	header, err := parseResponseHeader(m)
	if err != nil {
		return TransferRWIResponse{}, err
	}

	pause, err := optionalInt(FieldPause, m[FieldPause])
	if err != nil {
		return TransferRWIResponse{}, err
	}

	unknown, err := splitHashes("transferRWI response", FieldUnknownURL, m[FieldUnknownURL])
	if err != nil {
		return TransferRWIResponse{}, err
	}

	errorURL, err := splitHashes("transferRWI response", FieldErrorURL, m[FieldErrorURL])
	if err != nil {
		return TransferRWIResponse{}, err
	}

	result, err := parseTransferRWIResult(m[FieldResult])
	if err != nil {
		return TransferRWIResponse{}, err
	}

	return TransferRWIResponse{
		ResponseHeader: header,
		Result:         result,
		Pause:          pause,
		UnknownURL:     unknown,
		ErrorURL:       errorURL,
	}, nil
}

func encodeRWILines(entries []yacymodel.RWIPosting) string {
	lines := make([]string, len(entries))
	for i, entry := range entries {
		lines[i] = entry.String()
	}

	return strings.Join(lines, "\n")
}

const maxRWIEntries = 1000

func parseRWILines(raw string) []yacymodel.RWIPosting {
	if raw == "" {
		return nil
	}

	var entries []yacymodel.RWIPosting
	for line := range strings.SplitSeq(raw, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		if len(entries) >= maxRWIEntries {
			slog.WarnContext(
				context.Background(),
				"transfer rwi posting limit reached",
				slog.Int("limit", maxRWIEntries),
			)
			break
		}

		entry, err := yacymodel.ParseRWIPosting(line)
		if err != nil {
			slog.WarnContext(
				context.Background(),
				"transfer rwi posting discarded",
				slog.Any("error", err),
				slog.Int("lineLength", len(line)),
			)
			continue
		}
		if _, err := entry.URLHash(); err != nil {
			slog.WarnContext(
				context.Background(),
				"transfer rwi posting discarded",
				slog.Any("error", err),
				slog.Int("lineLength", len(line)),
			)
			continue
		}

		entries = append(entries, entry)
	}

	return entries
}
