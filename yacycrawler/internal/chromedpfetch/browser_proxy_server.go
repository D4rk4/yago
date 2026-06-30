package chromedpfetch

import "github.com/chromedp/chromedp"

func proxyExecAllocatorOptions(proxyURL string) []chromedp.ExecAllocatorOption {
	return []chromedp.ExecAllocatorOption{chromedp.ProxyServer(proxyURL)}
}
