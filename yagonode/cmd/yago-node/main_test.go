package main

import (
	"bytes"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/yagonode"
)

func TestMainRunsNode(t *testing.T) {
	ran := false
	oldRunNode := runNode
	runNode = func() { ran = true }
	t.Cleanup(func() { runNode = oldRunNode })

	main()

	if !ran {
		t.Fatal("main did not run node")
	}
}

func TestPrintVersionReportsBuildVersion(t *testing.T) {
	for _, arg := range []string{"--version", "-version", "version"} {
		var buf bytes.Buffer
		if !printVersion([]string{arg}, &buf) {
			t.Fatalf("printVersion(%q) = false, want true", arg)
		}
		want := "yago-node " + yagonode.Version() + "\n"
		if buf.String() != want {
			t.Fatalf("printVersion(%q) wrote %q, want %q", arg, buf.String(), want)
		}
	}
}

func TestPrintVersionIgnoresOtherArgs(t *testing.T) {
	var buf bytes.Buffer
	for _, args := range [][]string{nil, {}, {"serve"}, {"-h"}} {
		if printVersion(args, &buf) {
			t.Fatalf("printVersion(%v) = true, want false", args)
		}
	}
	if buf.Len() != 0 {
		t.Fatalf("printVersion wrote %q for non-version args", buf.String())
	}
}
