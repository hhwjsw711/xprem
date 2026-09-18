// Package requestmeta carries server-resolved network metadata in request contexts.
package requestmeta

import "context"

// Metadata describes the request's client, independently of authentication or audit.
type Metadata struct {
	IP        string
	UserAgent string
}

type contextKey struct{}

// WithContext stores the metadata resolved by the HTTP middleware.
func WithContext(ctx context.Context, meta Metadata) context.Context {
	return context.WithValue(ctx, contextKey{}, meta)
}

// FromContext returns empty metadata outside an HTTP request.
func FromContext(ctx context.Context) Metadata {
	meta, _ := ctx.Value(contextKey{}).(Metadata)
	return meta
}
