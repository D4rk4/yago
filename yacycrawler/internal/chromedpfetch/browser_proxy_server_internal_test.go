package chromedpfetch

import "testing"

func TestProxyExecAllocatorOptionsPinsProxyServer(t *testing.T) {
	options := proxyExecAllocatorOptions("http://proxy:4750")
	if len(options) != 1 {
		t.Fatalf("options = %d, want 1", len(options))
	}
	if options[0] == nil {
		t.Fatal("proxy option is nil")
	}
}
