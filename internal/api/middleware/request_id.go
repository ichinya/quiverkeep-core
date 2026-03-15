package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
)

const (
	RequestIDHeader = "X-Request-Id"
)

type requestIDKey struct{}

func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get(RequestIDHeader)
		if requestID == "" {
			requestID = randomID()
		}

		w.Header().Set(RequestIDHeader, requestID)
		ctx := context.WithValue(r.Context(), requestIDKey{}, requestID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func RequestIDFromContext(ctx context.Context) string {
	value := ctx.Value(requestIDKey{})
	asString, ok := value.(string)
	if !ok {
		return ""
	}
	return asString
}

func randomID() string {
	buffer := make([]byte, 8)
	if _, err := rand.Read(buffer); err != nil {
		return "0000000000000000"
	}
	return hex.EncodeToString(buffer)
}
