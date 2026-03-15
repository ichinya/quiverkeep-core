package middleware

import (
	"net/http"
	"strings"

	apierrors "github.com/ichinya/quiverkeep-core/internal/api/errors"
	"github.com/ichinya/quiverkeep-core/internal/config"
	qerrors "github.com/ichinya/quiverkeep-core/internal/errors"
	"github.com/ichinya/quiverkeep-core/internal/logging"
)

func Auth(cfg config.Config, logger *logging.Logger, remoteMode bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestID := RequestIDFromContext(r.Context())

			if remoteMode && !cfg.HasToken() {
				logger.Warn("remote mode requires token",
					"component", "api",
					"operation", "auth",
					"request_id", requestID,
					"path", r.URL.Path,
				)
				apierrors.Write(w, qerrors.New(qerrors.CodeUnauthorized, "token is required in remote mode"))
				return
			}

			if !cfg.HasToken() {
				next.ServeHTTP(w, r)
				return
			}

			header := strings.TrimSpace(r.Header.Get("Authorization"))
			if !strings.HasPrefix(strings.ToLower(header), "bearer ") {
				logger.Warn("missing bearer token",
					"component", "api",
					"operation", "auth",
					"request_id", requestID,
					"path", r.URL.Path,
				)
				apierrors.Write(w, qerrors.New(qerrors.CodeUnauthorized, "missing bearer token"))
				return
			}

			token := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
			token = strings.TrimSpace(strings.TrimPrefix(token, "bearer "))
			expected := ""
			if cfg.Core.Token != nil {
				expected = strings.TrimSpace(*cfg.Core.Token)
			}
			if token == "" || token != expected {
				logger.Warn("invalid bearer token",
					"component", "api",
					"operation", "auth",
					"request_id", requestID,
					"path", r.URL.Path,
				)
				apierrors.Write(w, qerrors.New(qerrors.CodeUnauthorized, "invalid bearer token"))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
