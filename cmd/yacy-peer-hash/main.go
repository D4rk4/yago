// Command yacy-peer-hash prints a freshly generated YaCy peer hash.
package main

import (
	"crypto/rand"
	"fmt"
	"io"
	"os"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

func main() {
	if err := run(os.Stdout, rand.Reader); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
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
