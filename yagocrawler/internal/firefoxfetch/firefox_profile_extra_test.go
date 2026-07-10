package firefoxfetch

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var errFakeWrite = errors.New("fake write failure")

func TestProxyHostPortRejectsUnparseableURL(t *testing.T) {
	if _, _, err := proxyHostPort("http://\x7f"); err == nil ||
		!strings.Contains(err.Error(), "parse browser proxy url") {
		t.Fatalf("error = %v, want a parse failure", err)
	}
}

func TestWriteFirefoxProfileErrorsWhenTempDirMissing(t *testing.T) {
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "does-not-exist"))
	if _, err := writeFirefoxProfile(firefoxProfile{MarionettePort: 2828}); err == nil ||
		!strings.Contains(err.Error(), "create firefox profile") {
		t.Fatalf("error = %v, want a temp-dir failure", err)
	}
}

func TestWriteFirefoxProfileErrorsOnBadProxy(t *testing.T) {
	if _, err := writeFirefoxProfile(firefoxProfile{
		MarionettePort: 2828,
		ProxyURL:       "http://no-port-here",
	}); err == nil || !strings.Contains(err.Error(), "port") {
		t.Fatalf("error = %v, want a proxy-parse failure", err)
	}
}

func TestWriteFirefoxProfileErrorsWhenWriteFails(t *testing.T) {
	restore := writeFile
	t.Cleanup(func() { writeFile = restore })
	writeFile = func(string, []byte, os.FileMode) error { return errFakeWrite }

	if _, err := writeFirefoxProfile(firefoxProfile{MarionettePort: 2828}); err == nil ||
		!strings.Contains(err.Error(), "write firefox user.js") {
		t.Fatalf("error = %v, want a user.js write failure", err)
	}
}
