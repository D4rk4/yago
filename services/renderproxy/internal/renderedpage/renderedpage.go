package renderedpage

import "context"

type Page struct {
	StatusCode  int
	ContentType string
	Body        []byte
}

type Renderer interface {
	Render(ctx context.Context, targetURL string) (Page, error)
}
