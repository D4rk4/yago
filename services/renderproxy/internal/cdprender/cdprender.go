// Package cdprender renders pages by driving a browser over the Chrome DevTools Protocol.
package cdprender

import (
	"context"
	"fmt"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"

	"github.com/nikitakarpei/yacy-rwi-node/renderproxy/internal/renderedpage"
)

type Renderer struct {
	allocatorCtx context.Context
	allocatorEnd context.CancelFunc
}

func New(ctx context.Context, cdpURL string) *Renderer {
	allocatorCtx, allocatorEnd := chromedp.NewRemoteAllocator(ctx, cdpURL)
	return &Renderer{allocatorCtx: allocatorCtx, allocatorEnd: allocatorEnd}
}

func (r *Renderer) Close() {
	r.allocatorEnd()
}

func (r *Renderer) Render(ctx context.Context, targetURL string) (renderedpage.Page, error) {
	tabCtx, tabCancel := chromedp.NewContext(r.allocatorCtx)
	defer tabCancel()

	mainFrameResponses := watchMainFrameResponses(tabCtx, targetURL)

	var body string
	if err := chromedp.Run(tabCtx,
		chromedp.Navigate(targetURL),
		chromedp.OuterHTML("html", &body, chromedp.ByQuery),
	); err != nil {
		return renderedpage.Page{}, fmt.Errorf("render %s: %w", targetURL, err)
	}

	statusCode, contentType := 0, ""
	select {
	case response := <-mainFrameResponses:
		statusCode = int(response.Status)
		contentType = response.MimeType
	default:
	}

	return renderedpage.Page{
		StatusCode:  statusCode,
		ContentType: contentType,
		Body:        []byte(body),
	}, nil
}

func watchMainFrameResponses(ctx context.Context, targetURL string) <-chan *network.Response {
	responses := make(chan *network.Response, 1)

	chromedp.ListenTarget(ctx, func(event any) {
		received, ok := event.(*network.EventResponseReceived)
		if !ok || received.Response == nil || received.Response.URL != targetURL {
			return
		}
		select {
		case responses <- received.Response:
		default:
		}
	})

	return responses
}
