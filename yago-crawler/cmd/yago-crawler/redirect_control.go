package main

import "github.com/D4rk4/yago/yago-crawler/internal/pagefetch"

type maximumRedirectsPageSource interface {
	SetMaxRedirects(int)
}

type maximumRedirectsControl struct {
	limit  *redirectLimit
	source pagefetch.PageSource
}

func (control maximumRedirectsControl) Apply(maximum int) {
	control.limit.Set(maximum)
	if browser, ok := control.source.(maximumRedirectsPageSource); ok {
		browser.SetMaxRedirects(maximum)
	}
}
