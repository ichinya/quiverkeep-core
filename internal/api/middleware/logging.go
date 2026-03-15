package middleware

import (
	"net/http"
	"time"

	"github.com/ichinya/quiverkeep-core/internal/logging"
)

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (s *statusWriter) WriteHeader(statusCode int) {
	s.status = statusCode
	s.ResponseWriter.WriteHeader(statusCode)
}

func Logging(logger *logging.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			started := time.Now()
			requestID := RequestIDFromContext(r.Context())

			logger.Info("http request start",
				"component", "api",
				"operation", "request",
				"request_id", requestID,
				"method", r.Method,
				"path", r.URL.Path,
			)

			wrapped := &statusWriter{
				ResponseWriter: w,
				status:         http.StatusOK,
			}
			next.ServeHTTP(wrapped, r)

			duration := time.Since(started)
			logger.Info("http request finish",
				"component", "api",
				"operation", "request",
				"request_id", requestID,
				"method", r.Method,
				"path", r.URL.Path,
				"status", wrapped.status,
				"duration_ms", duration.Milliseconds(),
			)
		})
	}
}
