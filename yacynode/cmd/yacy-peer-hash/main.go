// Command yacy-peer-hash prints a freshly generated YaCy peer hash.
package main

import (
	"crypto/rand"
	"fmt"
	"io"
	"os"

	"github.com/D4rk4/yago/yacymodel"
)

var (
	commandOutput  io.Writer = os.Stdout
	commandErrors  io.Writer = os.Stderr
	commandEntropy io.Reader = rand.Reader
	exitProcess              = os.Exit
)

func main() {
	if err := run(commandOutput, commandEntropy); err != nil {
		_, _ = fmt.Fprintln(commandErrors, err)
		exitProcess(1)
	}
}

func run(out io.Writer, entropy io.Reader) error {
	hash, err := yacymodel.GenerateHash(entropy)
	if err != nil {
		return fmt.Errorf("generate peer hash: %w", err)
	}
	if _, err := fmt.Fprintln(out, hash); err != nil {
		return fmt.Errorf("write peer hash: %w", err)
	}
	return nil
}
