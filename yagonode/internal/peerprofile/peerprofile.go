package peerprofile

import (
	"context"
	"io/fs"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/D4rk4/yago/yagonode/internal/httpguard"
	"github.com/D4rk4/yago/yagonode/internal/nodeidentity"
	"github.com/D4rk4/yago/yagoproto"
)

const (
	profileFileName              = "SETTINGS/profile.txt"
	profileFileReadFailedMessage = "profile file read failed"
)

type Property struct {
	Key   string
	Value string
}

type Properties interface {
	Properties(ctx context.Context) []Property
}

type NoPeerProfile struct{}

func (NoPeerProfile) Properties(context.Context) []Property {
	return nil
}

type ProfileFile struct {
	files fs.FS
}

func NewProfileFile(dataDir string) ProfileFile {
	return ProfileFile{files: os.DirFS(dataDir)}
}

func (p ProfileFile) Properties(ctx context.Context) []Property {
	if err := ctx.Err(); err != nil {
		return nil
	}

	raw, err := fs.ReadFile(p.files, profileFileName)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.WarnContext(ctx, profileFileReadFailedMessage, slog.Any("error", err))
		}

		return nil
	}

	return parseProfileProperties(string(raw))
}

func Mount(
	router httpguard.WireRouter,
	identity nodeidentity.Identity,
	profile Properties,
) {
	if profile == nil {
		profile = NoPeerProfile{}
	}

	httpguard.MountRaw(
		router,
		yagoproto.PathProfile,
		yagoproto.ProfileEndpointMethods,
		yagoproto.ParseProfileRequest,
		endpoint{identity: identity, profile: profile}.Serve,
	)
}

func parseProfileProperties(raw string) []Property {
	var properties []Property
	for line := range strings.Lines(raw) {
		key, value, ok := profilePropertyParts(line)
		if !ok {
			continue
		}
		properties = append(properties, Property{Key: key, Value: value})
	}

	return properties
}

func profilePropertyParts(line string) (string, string, bool) {
	line = strings.TrimRight(line, "\r\n")
	line = strings.TrimLeft(line, " \t\f")
	if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
		return "", "", false
	}

	key, value, ok := splitProfileProperty(line)
	if !ok {
		return "", "", false
	}

	key = strings.TrimSpace(unescapeProfileProperty(key))
	value = strings.TrimSpace(unescapeProfileProperty(value))
	if key == "" || value == "" {
		return "", "", false
	}

	return key, value, true
}

func splitProfileProperty(line string) (string, string, bool) {
	escaped := false
	for i, r := range line {
		if escaped {
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		if r == '=' || r == ':' {
			return line[:i], profilePropertyValue(line, i+1), true
		}
		if r == ' ' || r == '\t' || r == '\f' {
			return line[:i], profilePropertyValueAfterWhitespace(line, i+1), true
		}
	}

	return "", "", false
}

func profilePropertyValue(line string, start int) string {
	return line[profilePropertyValueStart(line, start):]
}

func profilePropertyValueAfterWhitespace(line string, start int) string {
	start = profilePropertyValueStart(line, start)
	if start < len(line) && (line[start] == '=' || line[start] == ':') {
		start++
	}

	return profilePropertyValue(line, start)
}

func profilePropertyValueStart(line string, start int) int {
	for start < len(line) {
		if line[start] != ' ' && line[start] != '\t' && line[start] != '\f' {
			return start
		}
		start++
	}

	return start
}

func unescapeProfileProperty(value string) string {
	var out strings.Builder
	for i := 0; i < len(value); i++ {
		if value[i] != '\\' || i+1 >= len(value) {
			out.WriteByte(value[i])
			continue
		}
		i++
		switch value[i] {
		case 'n':
			out.WriteByte('\n')
		case 'r':
			out.WriteByte('\r')
		case 't':
			out.WriteByte('\t')
		case 'f':
			out.WriteByte('\f')
		case 'u':
			if r, ok := profileUnicodeEscape(value[i+1:]); ok {
				out.WriteRune(r)
				i += 4
			} else {
				out.WriteString(`\u`)
			}
		default:
			out.WriteByte(value[i])
		}
	}

	return out.String()
}

func profileUnicodeEscape(value string) (rune, bool) {
	if len(value) < 4 {
		return 0, false
	}

	hex, err := strconv.ParseUint(value[:4], 16, 16)
	if err != nil {
		return 0, false
	}

	return utf16.Decode([]uint16{uint16(hex)})[0], true
}
