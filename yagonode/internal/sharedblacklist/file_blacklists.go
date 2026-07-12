package sharedblacklist

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
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
	files                 fs.FS
	maximumAggregateBytes int
}

func NewFileBlacklists(dataDir string) FileBlacklists {
	return FileBlacklists{files: os.DirFS(dataDir)}
}

func (b FileBlacklists) SharedList(ctx context.Context, name string) string {
	list, err := b.sharedList(ctx, name)
	if err != nil {
		return ""
	}

	return list
}

func (b FileBlacklists) sharedList(ctx context.Context, name string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("shared blacklist context: %w", err)
	}
	retention := newSharedBlacklistRetention(b.maximumAggregateBytes)
	shared, configured, err := b.sharedNames(ctx, retention)
	if err != nil || !configured {
		return "", err
	}

	var out strings.Builder
	for _, selected := range selectedSharedNames(shared, name) {
		if err := ctx.Err(); err != nil {
			return "", fmt.Errorf("shared blacklist context: %w", err)
		}
		if !validSharedBlacklistName(selected) {
			continue
		}
		if err := b.appendList(ctx, selected, retention, &out); err != nil {
			return "", err
		}
		if err := appendSharedBlacklistResponse(
			&out,
			retention,
			sharedBlacklistLineBreak,
		); err != nil {
			return "", err
		}
	}

	return out.String(), nil
}

func (b FileBlacklists) sharedNames(
	ctx context.Context,
	retention *sharedBlacklistRetention,
) ([]string, bool, error) {
	file, err := b.files.Open(sharedBlacklistConfigFileName)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.WarnContext(ctx, blacklistConfigReadFailed, slog.Any("error", err))
		}

		return nil, false, nil
	}
	defer func() { _ = file.Close() }()
	reader := bufio.NewReader(sharedBlacklistReader{ctx: ctx, source: file, retention: retention})
	for {
		line, readErr := reader.ReadString('\n')
		if err := ctx.Err(); err != nil {
			return nil, false, fmt.Errorf("shared blacklist config context: %w", err)
		}
		names, found, err := configuredSharedBlacklistNames(line, retention)
		if err != nil || found {
			return names, found, err
		}
		if readErr == nil {
			continue
		}

		configured, err := sharedBlacklistConfigReadResult(ctx, readErr)

		return nil, configured, err
	}
}

func configuredSharedBlacklistNames(
	line string,
	retention *sharedBlacklistRetention,
) ([]string, bool, error) {
	if line == "" {
		return nil, false, nil
	}
	key, value, ok := settingParts(line)
	if !ok || key != sharedBlacklistConfigKey {
		return nil, false, nil
	}
	if err := retention.retain(len(line)); err != nil {
		return nil, true, err
	}
	names, err := retainedSharedBlacklistNames(value, retention)

	return names, true, err
}

func sharedBlacklistConfigReadResult(
	ctx context.Context,
	readErr error,
) (bool, error) {
	if errors.Is(readErr, io.EOF) {
		return true, nil
	}
	if errors.Is(readErr, errSharedBlacklistBudgetExceeded) ||
		errors.Is(readErr, context.Canceled) || errors.Is(readErr, context.DeadlineExceeded) {
		return false, fmt.Errorf("read shared blacklist config: %w", readErr)
	}
	slog.WarnContext(ctx, blacklistConfigReadFailed, slog.Any("error", readErr))

	return false, nil
}

func (b FileBlacklists) appendList(
	ctx context.Context,
	name string,
	retention *sharedBlacklistRetention,
	out *strings.Builder,
) error {
	if !validSharedBlacklistName(name) {
		return nil
	}
	file, err := b.files.Open(path.Join(sharedBlacklistDirectoryName, name))
	if err != nil {
		if !os.IsNotExist(err) {
			slog.WarnContext(
				ctx,
				blacklistFileReadFailedMessage,
				slog.String("name", name),
				slog.Any("error", err),
			)
		}

		return nil
	}
	defer func() { _ = file.Close() }()
	reader := bufio.NewReader(sharedBlacklistReader{ctx: ctx, source: file, retention: retention})
	wrote := false
	for {
		line, readErr := reader.ReadString('\n')
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("shared blacklist file context: %w", err)
		}
		retained, err := appendSharedBlacklistLine(out, retention, line)
		if err != nil {
			return err
		}
		wrote = wrote || retained
		if readErr == nil {
			continue
		}

		return sharedBlacklistFileReadResult(ctx, name, wrote, readErr)
	}
}

func appendSharedBlacklistLine(
	out *strings.Builder,
	retention *sharedBlacklistRetention,
	line string,
) (bool, error) {
	line = strings.TrimRight(line, "\r\n")
	if line == "" || strings.HasPrefix(line, "#") {
		return false, nil
	}
	if err := retention.retain(len(line) + len(sharedBlacklistLineBreak)); err != nil {
		return false, err
	}
	out.WriteString(line)
	out.WriteString(sharedBlacklistLineBreak)

	return true, nil
}

func sharedBlacklistFileReadResult(
	ctx context.Context,
	name string,
	wrote bool,
	readErr error,
) error {
	if errors.Is(readErr, io.EOF) {
		return nil
	}
	if errors.Is(readErr, errSharedBlacklistBudgetExceeded) ||
		errors.Is(readErr, context.Canceled) || errors.Is(readErr, context.DeadlineExceeded) {
		return fmt.Errorf("read shared blacklist file %q: %w", name, readErr)
	}
	slog.WarnContext(
		ctx,
		blacklistFileReadFailedMessage,
		slog.String("name", name),
		slog.Any("error", readErr),
	)
	if wrote {
		return fmt.Errorf("read shared blacklist file %q: %w", name, readErr)
	}

	return nil
}

func (b FileBlacklists) listText(ctx context.Context, name string) string {
	retention := newSharedBlacklistRetention(b.maximumAggregateBytes)
	var out strings.Builder
	if err := b.appendList(ctx, name, retention, &out); err != nil {
		return ""
	}

	return out.String()
}

func appendSharedBlacklistResponse(
	out *strings.Builder,
	retention *sharedBlacklistRetention,
	value string,
) error {
	if err := retention.retain(len(value)); err != nil {
		return err
	}
	out.WriteString(value)

	return nil
}

func retainedSharedBlacklistNames(
	raw string,
	retention *sharedBlacklistRetention,
) ([]string, error) {
	names := make([]string, 0, min(8, maximumSharedBlacklistFiles))
	var retainErr error
	visitSharedBlacklistNames(raw, func(name string) bool {
		if len(names) >= maximumSharedBlacklistFiles {
			retainErr = fmt.Errorf(
				"%w: maximum %d configured lists",
				errSharedBlacklistBudgetExceeded,
				maximumSharedBlacklistFiles,
			)

			return false
		}
		if err := retention.retain(len(name) + retainedSharedBlacklistNameBytes); err != nil {
			retainErr = err

			return false
		}
		names = append(names, strings.Clone(name))

		return true
	})

	return names, retainErr
}

func selectedSharedNames(names []string, requested string) []string {
	if requested == "" {
		return names
	}

	for position, name := range names {
		if name == requested {
			return names[position : position+1]
		}
	}

	return nil
}

func visitSharedBlacklistNames(raw string, visit func(string) bool) {
	for {
		name, remainder, more := strings.Cut(raw, ",")
		name = strings.TrimSpace(name)
		if name != "" && !visit(name) {
			return
		}
		if !more {
			return
		}
		raw = remainder
	}
}

func validSharedBlacklistName(name string) bool {
	return name != "" &&
		name != "." &&
		name != ".." &&
		!strings.ContainsAny(name, `/\`)
}

func sharedBlacklistText(raw string) string {
	retention := newSharedBlacklistRetention(maximumSharedBlacklistAggregateBytes)
	if err := retention.retain(len(raw)); err != nil {
		return ""
	}
	var out strings.Builder
	for line := range strings.Lines(raw) {
		if _, err := appendSharedBlacklistLine(&out, retention, line); err != nil {
			return ""
		}
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
