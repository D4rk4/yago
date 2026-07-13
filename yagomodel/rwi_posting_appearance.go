package yagomodel

import (
	"context"
	"log/slog"
)

const RWIFlagBitCount = 32

const (
	RWIFlagHasImage = 20
	RWIFlagHasAudio = 21
	RWIFlagHasVideo = 22
	RWIFlagHasApp   = 23
)

const (
	DocTypeText  = 't'
	DocTypeImage = 'i'
	DocTypeAudio = 'a'
	DocTypeMovie = 'm'
)

func (e RWIPosting) DocType() (byte, bool) {
	if _, ok := e.Properties[ColDocType]; !ok {
		return 0, false
	}
	value, err := e.ByteValue(ColDocType)
	if err != nil {
		slog.WarnContext(context.Background(), "rwi doctype discarded", slog.Any("error", err))
		return 0, false
	}
	return value, true
}

func (e RWIPosting) AppearanceFlags() (Bitfield, error) {
	value, ok := e.Properties[ColFlags]
	if !ok {
		return nil, nil
	}
	return DecodeBitfield(value)
}
