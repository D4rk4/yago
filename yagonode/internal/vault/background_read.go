package vault

import "context"

type backgroundReadContextKey struct{}

func BackgroundRead(ctx context.Context) context.Context {
	return context.WithValue(ctx, backgroundReadContextKey{}, true)
}

func IsBackgroundRead(ctx context.Context) bool {
	background, _ := ctx.Value(backgroundReadContextKey{}).(bool)

	return background
}
