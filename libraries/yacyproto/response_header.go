package yacyproto

import "github.com/nikitakarpei/yacy-rwi-node/yacymodel"

type ResponseHeader struct {
	Version string
	Uptime  int
}

func InjectResponseHeader(dst yacymodel.Message, version string, uptime int) {
	setString(dst, FieldVersion, version)
	setInt(dst, FieldUptime, uptime)
}

func parseResponseHeader(m yacymodel.Message) (ResponseHeader, error) {
	uptime, err := optionalInt(FieldUptime, m[FieldUptime])
	if err != nil {
		return ResponseHeader{}, err
	}

	return ResponseHeader{Version: m[FieldVersion], Uptime: uptime}, nil
}
