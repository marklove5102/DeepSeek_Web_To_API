package shared

import "context"

type skipCurrentInputFileContextKey struct{}

func WithCurrentInputFileSkipped(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, skipCurrentInputFileContextKey{}, true)
}

func CurrentInputFileSkipped(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	v, _ := ctx.Value(skipCurrentInputFileContextKey{}).(bool)
	return v
}
