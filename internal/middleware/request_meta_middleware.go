package middleware

import (
	"net/http"
	"xprem/internal/helpers"
	"xprem/internal/requestmeta"
)

// RequestMetaMiddleware stamps the client IP (proxy-aware, see
// helpers.ClientIP) and user agent on every request's context for downstream services.
func RequestMetaMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		meta := requestmeta.Metadata{UserAgent: r.UserAgent()}
		if addr := helpers.ClientIP(r); addr.IsValid() {
			meta.IP = addr.String()
		}
		next.ServeHTTP(w, r.WithContext(requestmeta.WithContext(r.Context(), meta)))
	})
}
