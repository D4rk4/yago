package documentstore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const (
	storedDocumentResumeCursorVersion = 1
	storedDocumentResumeCursorLimit   = 96 << 10
	storedDocumentResumeKeyLimit      = maximumStoredDocumentURLIdentityBytes + orderedDocumentAdmissionSize
)

type ResumableStoredDocumentPageScanner interface {
	ScanStoredDocumentPagesFrom(
		ctx context.Context,
		cursor string,
		visit func(*Document, string, bool) (bool, error),
	) error
}

type storedDocumentResumeCursor struct {
	Version    int      `json:"version"`
	Partition  int      `json:"partition"`
	After      []byte   `json:"after,omitempty"`
	Boundaries [][]byte `json:"boundaries"`
}

type storedDocumentPageVisit struct {
	ctx           context.Context
	visit         func(*Document, string, bool) (bool, error)
	state         storedDocumentResumeCursor
	boundary      storedDocumentPartitionBoundary
	page          storedDocumentRawPage
	boundaryCount int
}

type visibleStoredDocumentVisit struct {
	ctx           context.Context
	visit         func(*Document, string, bool) (bool, error)
	state         storedDocumentResumeCursor
	visible       []storedDocumentPageEntry
	pageComplete  bool
	boundaryCount int
}

func (d documentVault) ScanStoredDocumentPagesFrom(
	ctx context.Context,
	cursor string,
	visit func(*Document, string, bool) (bool, error),
) error {
	release, err := d.enterStoredDocumentScan(ctx)
	if err != nil {
		return fmt.Errorf("enter resumable document scan: %w", err)
	}
	defer release()
	state, err := d.decodeStoredDocumentResumeCursor(ctx, cursor)
	if err != nil {
		return err
	}
	boundaries := []storedDocumentPartitionBoundary{
		{partition: legacyDocumentPartition, bucket: bucketName},
		{partition: orderedDocumentPartition, bucket: orderedDocumentBucketName},
	}
	return d.scanStoredDocumentPartitions(ctx, visit, state, boundaries)
}

func (d documentVault) scanStoredDocumentPartitions(
	ctx context.Context,
	visit func(*Document, string, bool) (bool, error),
	state storedDocumentResumeCursor,
	boundaries []storedDocumentPartitionBoundary,
) error {
	for state.Partition < len(boundaries) {
		var stop bool
		var err error
		state, stop, err = d.scanResumableStoredDocumentPartition(ctx, visit, state, boundaries)
		if err != nil || stop {
			return err
		}
	}
	finalToken, err := encodeStoredDocumentResumeCursor(state)
	if err != nil {
		return err
	}
	_, err = visit(nil, finalToken, true)
	return err
}

func (d documentVault) scanResumableStoredDocumentPartition(
	ctx context.Context,
	visit func(*Document, string, bool) (bool, error),
	state storedDocumentResumeCursor,
	boundaries []storedDocumentPartitionBoundary,
) (storedDocumentResumeCursor, bool, error) {
	if err := ctx.Err(); err != nil {
		return state, false, fmt.Errorf("context: %w", err)
	}
	boundary := boundaries[state.Partition]
	boundary.lastKey = vault.Key(state.Boundaries[state.Partition])
	if len(boundary.lastKey) == 0 {
		state.Partition++
		state.After = nil
		return state, false, nil
	}
	page, err := d.readStoredDocumentRawPage(
		ctx,
		boundary,
		vault.Key(state.After),
		storedDocumentPageSize,
	)
	if err != nil {
		return state, false, err
	}
	return d.visitStoredDocumentPage(storedDocumentPageVisit{
		ctx: ctx, visit: visit, state: state, boundary: boundary,
		page: page, boundaryCount: len(boundaries),
	})
}

func (d documentVault) visitStoredDocumentPage(
	input storedDocumentPageVisit,
) (storedDocumentResumeCursor, bool, error) {
	authoritative, err := d.authoritativeStoredDocumentRawPage(
		input.ctx,
		input.boundary.partition,
		input.page.entries,
	)
	if err != nil {
		return input.state, false, err
	}
	entries, err := decodeStoredDocumentRawPage(input.ctx, authoritative)
	if err != nil {
		return input.state, false, err
	}
	visible, err := resumableVisibleDocuments(input.ctx, input.boundary.partition, entries)
	if err != nil {
		return input.state, false, err
	}
	if len(visible) == 0 {
		next := resumeCursorAfterPage(input.state, input.page, input.boundaryCount)
		keepGoing, err := d.visitResumableDocument(
			input.ctx,
			input.visit,
			nil,
			next,
			input.boundaryCount,
		)
		return next, !keepGoing || next.Partition == input.boundaryCount, err
	}
	return d.visitVisibleStoredDocuments(visibleStoredDocumentVisit{
		ctx: input.ctx, visit: input.visit, state: input.state, visible: visible,
		pageComplete: input.page.complete, boundaryCount: input.boundaryCount,
	})
}

func (d documentVault) visitVisibleStoredDocuments(
	input visibleStoredDocumentVisit,
) (storedDocumentResumeCursor, bool, error) {
	state := input.state
	for index := range input.visible {
		next := nextVisibleDocumentCursor(state, input.visible, index, input.pageComplete)
		keepGoing, err := d.visitResumableDocument(
			input.ctx,
			input.visit,
			&input.visible[index].document,
			next,
			input.boundaryCount,
		)
		if err != nil || !keepGoing || next.Partition == input.boundaryCount {
			return next, true, err
		}
		state = next
	}
	return state, false, nil
}

func nextVisibleDocumentCursor(
	state storedDocumentResumeCursor,
	visible []storedDocumentPageEntry,
	index int,
	pageComplete bool,
) storedDocumentResumeCursor {
	next := state
	if pageComplete && index == len(visible)-1 {
		next.Partition++
		next.After = nil
	} else {
		next.After = append([]byte(nil), visible[index].key...)
	}
	return next
}

func (d documentVault) visitResumableDocument(
	ctx context.Context,
	visit func(*Document, string, bool) (bool, error),
	document *Document,
	state storedDocumentResumeCursor,
	boundaryCount int,
) (bool, error) {
	token, err := encodeStoredDocumentResumeCursor(state)
	if err != nil {
		return false, err
	}
	if err := ctx.Err(); err != nil {
		return false, fmt.Errorf("resumable document scan context: %w", err)
	}
	return visit(document, token, state.Partition == boundaryCount)
}

func resumeCursorAfterPage(
	state storedDocumentResumeCursor,
	page storedDocumentRawPage,
	boundaryCount int,
) storedDocumentResumeCursor {
	next := state
	if page.complete {
		next.Partition++
		next.After = nil
	} else {
		next.After = append([]byte(nil), page.cursor...)
	}
	if next.Partition > boundaryCount {
		next.Partition = boundaryCount
	}
	return next
}

func (d documentVault) decodeStoredDocumentResumeCursor(
	ctx context.Context,
	token string,
) (storedDocumentResumeCursor, error) {
	if token == "" {
		boundaries, err := d.captureStoredDocumentPartitionBoundaries(ctx)
		if err != nil {
			return storedDocumentResumeCursor{}, err
		}
		state := storedDocumentResumeCursor{Version: storedDocumentResumeCursorVersion}
		for _, boundary := range boundaries {
			state.Boundaries = append(state.Boundaries, append([]byte(nil), boundary.lastKey...))
		}
		return state, nil
	}
	if len(token) > storedDocumentResumeCursorLimit {
		return storedDocumentResumeCursor{}, errors.New(
			"stored document resume cursor exceeds size limit",
		)
	}
	decoder := json.NewDecoder(strings.NewReader(token))
	decoder.DisallowUnknownFields()
	var state storedDocumentResumeCursor
	if err := decoder.Decode(&state); err != nil {
		return storedDocumentResumeCursor{}, fmt.Errorf(
			"decode stored document resume cursor: %w",
			err,
		)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return storedDocumentResumeCursor{}, errors.New(
			"stored document resume cursor contains trailing data",
		)
	}
	if err := validateStoredDocumentResumeCursor(state); err != nil {
		return storedDocumentResumeCursor{}, err
	}
	if state.Partition < len(state.Boundaries) &&
		bytes.Compare(state.After, state.Boundaries[state.Partition]) > 0 {
		return storedDocumentResumeCursor{}, errors.New(
			"stored document resume key exceeds its partition boundary",
		)
	}
	return state, nil
}

func encodeStoredDocumentResumeCursor(state storedDocumentResumeCursor) (string, error) {
	if err := validateStoredDocumentResumeCursor(state); err != nil {
		return "", err
	}
	encoded, _ := json.Marshal(state)
	return string(encoded), nil
}

func validateStoredDocumentResumeCursor(state storedDocumentResumeCursor) error {
	if state.Version != storedDocumentResumeCursorVersion || len(state.Boundaries) != 2 ||
		state.Partition < 0 || state.Partition > len(state.Boundaries) {
		return errors.New("invalid stored document resume cursor")
	}
	if len(state.After) > storedDocumentResumeKeyLimit {
		return errors.New("stored document resume key exceeds size limit")
	}
	for _, boundary := range state.Boundaries {
		if len(boundary) > storedDocumentResumeKeyLimit {
			return errors.New("stored document boundary exceeds size limit")
		}
	}
	return nil
}

func resumableVisibleDocuments(
	ctx context.Context,
	partition storedDocumentPartition,
	entries []storedDocumentPageEntry,
) ([]storedDocumentPageEntry, error) {
	documents := make([]storedDocumentPageEntry, 0, len(entries))
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("resumable visible document context: %w", err)
		}
		normalizedURL := string(entry.key)
		if partition == orderedDocumentPartition {
			var err error
			_, normalizedURL, err = decodeOrderedDocumentKey(entry.key)
			if err != nil {
				continue
			}
		}
		if entry.document.NormalizedURL == normalizedURL {
			documents = append(documents, entry)
		}
	}
	return documents, nil
}

var _ ResumableStoredDocumentPageScanner = documentVault{}
