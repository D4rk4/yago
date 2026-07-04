package sharedblacklist

import (
	"context"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"strings"
)

const (
	sharedBlacklistConfigFileName  = "SETTINGS/yacy.conf"
	sharedBlacklistDirectoryName   = "LISTS"
	sharedBlacklistConfigKey       = "BlackLists.Shared"
	sharedBlacklistLineBreak       = "\r\n"
	blacklistConfigReadFailed      = "shared blacklist config read failed"
	blacklistFileReadFailedMessage = "shared blacklist file read failed"
)

type FileBlacklists struct {
	files fs.FS
}

func NewFileBlacklists(dataDir string) FileBlacklists {
	return FileBlacklists{files: os.DirFS(dataDir)}
}

func (b FileBlacklists) SharedList(ctx context.Context, name string) string {
	if err := ctx.Err(); err != nil {
		return ""
	}

	shared, ok := b.sharedNames(ctx)
	if !ok {
		return ""
	}

	var out strings.Builder
	for _, selected := range selectedSharedNames(shared, name) {
		if !validSharedBlacklistName(selected) {
			continue
		}
		out.WriteString(b.listText(ctx, selected))
		out.WriteString(sharedBlacklistLineBreak)
	}

	return out.String()
}

func (b FileBlacklists) sharedNames(ctx context.Context) ([]string, bool) {
	raw, err := fs.ReadFile(b.files, sharedBlacklistConfigFileName)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.WarnContext(ctx, blacklistConfigReadFailed, slog.Any("error", err))
		}

		return nil, false
	}

	return sharedBlacklistNames(settingValue(string(raw), sharedBlacklistConfigKey)), true
}

func (b FileBlacklists) listText(ctx context.Context, name string) string {
	if !validSharedBlacklistName(name) {
		return ""
	}

	raw, err := fs.ReadFile(b.files, path.Join(sharedBlacklistDirectoryName, name))
	if err != nil {
		if !os.IsNotExist(err) {
			slog.WarnContext(
				ctx,
				blacklistFileReadFailedMessage,
				slog.String("name", name),
				slog.Any("error", err),
			)
		}

		return ""
	}

	return sharedBlacklistText(string(raw))
}

func selectedSharedNames(names []string, requested string) []string {
	if requested == "" {
		return names
	}

	for _, name := range names {
		if name == requested {
			return []string{name}
		}
	}

	return nil
}

func sharedBlacklistNames(raw string) []string {
	var names []string
	for _, name := range strings.Split(raw, ",") {
		name = strings.TrimSpace(name)
		if name != "" {
			names = append(names, name)
		}
	}

	return names
}

func validSharedBlacklistName(name string) bool {
	return name != "" &&
		name != "." &&
		name != ".." &&
		!strings.ContainsAny(name, `/\`)
}

func sharedBlacklistText(raw string) string {
	var out strings.Builder
	for line := range strings.Lines(raw) {
		line = strings.TrimRight(line, "\r\n")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out.WriteString(line)
		out.WriteString(sharedBlacklistLineBreak)
	}

	return out.String()
}

func settingValue(raw string, key string) string {
	for line := range strings.Lines(raw) {
		if k, value, ok := settingParts(line); ok && k == key {
			return value
		}
	}

	return ""
}

func settingParts(line string) (string, string, bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}

	index := settingSeparator(line)
	if index < 1 {
		return "", "", false
	}

	return unescapeSetting(line[:index]), unescapeSetting(line[index+1:]), true
}

func settingSeparator(line string) int {
	for index, escaped := 0, false; index < len(line); index++ {
		if escaped {
			escaped = false
			continue
		}
		if line[index] == '\\' {
			escaped = true
			continue
		}
		if line[index] == '=' {
			return index
		}
	}

	return -1
}

func unescapeSetting(value string) string {
	var out strings.Builder
	value = strings.TrimSpace(value)
	for index := 0; index < len(value); index++ {
		if value[index] != '\\' || index+1 >= len(value) {
			out.WriteByte(value[index])
			continue
		}
		index++
		out.WriteString(settingEscape(value[index]))
	}

	return out.String()
}

func settingEscape(value byte) string {
	switch value {
	case 'n':
		return "\n"
	case '=':
		return "="
	case '\\':
		return "\\"
	default:
		return `\` + string(value)
	}
}
