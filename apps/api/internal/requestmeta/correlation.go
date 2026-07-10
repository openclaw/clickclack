package requestmeta

import (
	"context"
	"strings"
)

type correlationIDContextKey struct{}

func ValidCorrelationID(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._:-", r) {
			continue
		}
		return false
	}
	return true
}

func WithCorrelationID(ctx context.Context, value string) context.Context {
	if !ValidCorrelationID(value) {
		return ctx
	}
	return context.WithValue(ctx, correlationIDContextKey{}, value)
}

func CorrelationID(ctx context.Context) string {
	value, _ := ctx.Value(correlationIDContextKey{}).(string)
	if !ValidCorrelationID(value) {
		return ""
	}
	return value
}
