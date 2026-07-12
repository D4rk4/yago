package peerprofile

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
)

const maximumProfileSourceBytes = 1 << 20

var errProfileSourceTooLarge = errors.New("profile source exceeds limit")

type profileContextReader struct {
	ctx    context.Context
	source io.Reader
}

func (r profileContextReader) Read(destination []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, fmt.Errorf("profile read context: %w", err)
	}
	read, err := r.source.Read(destination)
	if err == nil {
		return read, nil
	}
	if errors.Is(err, io.EOF) {
		return read, io.EOF
	}

	return read, fmt.Errorf("read profile source: %w", err)
}

func readProfileSource(ctx context.Context, files fs.FS) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("profile read context: %w", err)
	}
	file, err := files.Open(profileFileName)
	if err != nil {
		return nil, fmt.Errorf("open profile source: %w", err)
	}
	defer func() { _ = file.Close() }()
	limited := &io.LimitedReader{
		R: profileContextReader{ctx: ctx, source: file},
		N: maximumProfileSourceBytes + 1,
	}
	raw, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("load profile source: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("profile read context: %w", err)
	}
	if len(raw) > maximumProfileSourceBytes {
		return nil, errProfileSourceTooLarge
	}

	return raw, nil
}
