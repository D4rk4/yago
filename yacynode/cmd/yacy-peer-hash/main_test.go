package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yacymodel"
)

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

func TestMainPrintsValidPeerHash(t *testing.T) {
	savedOutput := commandOutput
	savedErrors := commandErrors
	savedEntropy := commandEntropy
	savedExit := exitProcess
	t.Cleanup(func() {
		commandOutput = savedOutput
		commandErrors = savedErrors
		commandEntropy = savedEntropy
		exitProcess = savedExit
	})

	var out bytes.Buffer
	commandOutput = &out
	commandErrors = &bytes.Buffer{}
	commandEntropy = bytes.NewReader(bytes.Repeat([]byte{0x24}, 32))
	exitProcess = func(code int) {
		t.Fatalf("unexpected exit %d", code)
	}

	main()

	hash, err := yacymodel.ParseHash(strings.TrimSpace(out.String()))
	if err != nil {
		t.Fatalf("parse printed hash: %v", err)
	}
	if !hash.Valid() {
		t.Fatalf("hash %q is not valid", hash)
	}
}

func TestMainReportsFailure(t *testing.T) {
	savedOutput := commandOutput
	savedErrors := commandErrors
	savedEntropy := commandEntropy
	savedExit := exitProcess
	t.Cleanup(func() {
		commandOutput = savedOutput
		commandErrors = savedErrors
		commandEntropy = savedEntropy
		exitProcess = savedExit
	})

	var errors bytes.Buffer
	var exitCode int
	commandOutput = &bytes.Buffer{}
	commandErrors = &errors
	commandEntropy = bytes.NewReader(nil)
	exitProcess = func(code int) {
		exitCode = code
	}

	main()

	if exitCode != 1 {
		t.Fatalf("exit code = %d, want 1", exitCode)
	}
	if !strings.Contains(errors.String(), "generate peer hash") {
		t.Fatalf("error output = %q, want generation failure", errors.String())
	}
}

func TestRunPrintsValidPeerHash(t *testing.T) {
	var out bytes.Buffer
	if err := run(&out, bytes.NewReader(bytes.Repeat([]byte{0x42}, 32))); err != nil {
		t.Fatalf("run: %v", err)
	}

	hash, err := yacymodel.ParseHash(strings.TrimSpace(out.String()))
	if err != nil {
		t.Fatalf("parse printed hash: %v", err)
	}
	if !hash.Valid() {
		t.Fatalf("hash %q is not valid", hash)
	}
}

func TestRunReturnsErrorOnShortEntropy(t *testing.T) {
	if err := run(&bytes.Buffer{}, bytes.NewReader(nil)); err == nil {
		t.Fatal("expected error for insufficient entropy")
	}
}

func TestRunReturnsErrorOnWriteFailure(t *testing.T) {
	if err := run(failingWriter{}, bytes.NewReader(bytes.Repeat([]byte{0x42}, 32))); err == nil {
		t.Fatal("expected write error")
	}
}
