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
			proxySpendingPath := requiresTokenForProxySpend(r.URL.Path)
			requiresToken := remoteMode || proxySpendingPath

			if requiresToken && !cfg.HasToken() {
				reason := "remote_mode"
				message := "token is required in remote mode"
				if proxySpendingPath {
					reason = "proxy_spend"
					message = "token is required for proxy operations"
				}

				logger.Warn("request requires token but token is not configured",
					"component", "api",
					"operation", "auth",
					"request_id", requestID,
					"path", r.URL.Path,
					"reason", reason,
				)
				apierrors.Write(w, qerrors.New(qerrors.CodeUnauthorized, message))
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

func requiresTokenForProxySpend(path string) bool {
	cleanPath := strings.TrimSpace(path)
	switch cleanPath {
	case "/api/v1/proxy/anthropic/messages":
		return true
	default:
		return false
	}
}
