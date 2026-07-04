package yagoproto

import "github.com/D4rk4/yago/yagomodel"

type ResponseHeader struct {
	Version string
	Uptime  int
}

func InjectResponseHeader(dst yagomodel.Message, version string, uptime int) {
	setString(dst, FieldVersion, version)
	setInt(dst, FieldUptime, uptime)
}

func parseResponseHeader(m yagomodel.Message) (ResponseHeader, error) {
	uptime, err := optionalInt(FieldUptime, m[FieldUptime])
	if err != nil {
		return ResponseHeader{}, err
	}

	return ResponseHeader{Version: m[FieldVersion], Uptime: uptime}, nil
}
