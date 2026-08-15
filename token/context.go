package token

import "context"

type claimsContextKey struct{}

func NewContext(ctx context.Context, claims *Claims) context.Context {
	return context.WithValue(ctx, claimsContextKey{}, claims)
}

func FromContext(ctx context.Context) (*Claims, bool) {
	claims, ok := ctx.Value(claimsContextKey{}).(*Claims)
	return claims, ok && claims != nil
}

func UserID(ctx context.Context) string {
	claims, ok := FromContext(ctx)
	if !ok {
		return ""
	}
	return claims.Subject
}

func HasRole(ctx context.Context, role string) bool {
	claims, ok := FromContext(ctx)
	return ok && claims.HasRole(role)
}
