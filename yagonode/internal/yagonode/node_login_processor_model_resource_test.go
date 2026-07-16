package yagonode

import (
	"errors"
	"io"
	"strings"
	"testing"
)

type processorInformationReadCloser struct {
	io.Reader
	closeErr error
	closed   bool
}

func (r *processorInformationReadCloser) Close() error {
	r.closed = true

	return r.closeErr
}

func TestProcessorModelOpenedInformationLifecycle(t *testing.T) {
	t.Parallel()

	openErr := errors.New("open failed")
	if _, err := processorModelFromOpenedInformation(nil, openErr); !errors.Is(err, openErr) {
		t.Fatalf("open error = %v", err)
	}

	success := &processorInformationReadCloser{
		Reader: strings.NewReader("model name : Test Processor\n"),
	}
	model, err := processorModelFromOpenedInformation(success, nil)
	if err != nil || model != "Test Processor" || !success.closed {
		t.Fatalf("successful processor read = %q, %v, closed=%t", model, err, success.closed)
	}

	closeErr := errors.New("close failed")
	closeFailure := &processorInformationReadCloser{
		Reader:   strings.NewReader("model name : Test Processor\n"),
		closeErr: closeErr,
	}
	if _, err := processorModelFromOpenedInformation(closeFailure, nil); !errors.Is(err, closeErr) {
		t.Fatalf("close error = %v", err)
	}

	scanFailure := &processorInformationReadCloser{
		Reader: strings.NewReader(strings.Repeat("x", maximumProcessorInformationLine+1)),
	}
	if _, err := processorModelFromOpenedInformation(
		scanFailure,
		nil,
	); err == nil || !scanFailure.closed {
		t.Fatalf("scan error = %v, closed=%t", err, scanFailure.closed)
	}
}
