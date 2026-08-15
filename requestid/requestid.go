package requestid

import (
	"context"

	"github.com/google/uuid"
)

const MetadataKey = "x-request-id"

type contextKey struct{}

func NewContext(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, contextKey{}, id)
}

func FromContext(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(contextKey{}).(string)
	return id, ok && id != ""
}

func Get(ctx context.Context) string {
	id, _ := FromContext(ctx)
	return id
}

func Generate() string {
	return uuid.NewString()
}
