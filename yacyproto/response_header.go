package yacyproto

import "github.com/nikitakarpei/yacy-rwi-node/yacymodel"

type ResponseHeader struct {
	Version string
	Uptime  int
}

func (h ResponseHeader) write(dst yacymodel.Message) {
	setString(dst, FieldVersion, h.Version)
	setInt(dst, FieldUptime, h.Uptime)
}

func parseResponseHeader(m yacymodel.Message) (ResponseHeader, error) {
	uptime, err := optionalInt(FieldUptime, m[FieldUptime])
	if err != nil {
		return ResponseHeader{}, err
	}

	return ResponseHeader{Version: m[FieldVersion], Uptime: uptime}, nil
}
