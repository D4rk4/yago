package main

import (
	"fmt"
	"io"
	"os"

	"github.com/D4rk4/yago/yagonode/internal/yagonode"
)

var runNode = yagonode.Main

func main() {
	if printVersion(os.Args[1:], os.Stdout) {
		return
	}
	runNode()
}

// printVersion answers `yago-node --version` (also -version/version) by writing
// the stamped build version, returning true when it handled the request so the
// node does not otherwise start.
func printVersion(args []string, out io.Writer) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "--version", "-version", "version":
		_, _ = fmt.Fprintln(out, "yago-node "+yagonode.Version())

		return true
	default:
		return false
	}
}
