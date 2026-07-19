package frontier

type URLDenylist interface {
	Blocks(string) bool
}

func WithURLDenylist(denylist URLDenylist) Option {
	return func(frontier *Frontier) {
		frontier.urlDenylist = denylist
	}
}

func (f *Frontier) urlDenied(rawURL string) bool {
	return f.urlDenylist != nil && f.urlDenylist.Blocks(rawURL)
}
