package yacymodel

import "log/slog"

const RWIFlagBitCount = 32

const (
	RWIFlagHasImage = 20
	RWIFlagHasAudio = 21
	RWIFlagHasVideo = 22
	RWIFlagHasApp   = 23
)

const (
	DocTypeImage = 'i'
	DocTypeAudio = 'a'
	DocTypeMovie = 'm'
)

func (e RWIEntry) DocType() (byte, bool) {
	raw, err := Decode(e.Properties[ColDocType])
	if err != nil {
		slog.Warn("rwi doctype discarded", "error", err)
		return 0, false
	}
	if len(raw) != 1 {
		return 0, false
	}
	return raw[0], true
}

func (e RWIEntry) AppearanceFlags() (Bitfield, error) {
	value, ok := e.Properties[ColFlags]
	if !ok {
		return nil, nil
	}
	return DecodeBitfield(value)
}
