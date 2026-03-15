package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ichinya/quiverkeep-core/internal/api/middleware"
	"github.com/ichinya/quiverkeep-core/internal/config"
	"github.com/ichinya/quiverkeep-core/internal/logging"
)

func TestAuthMatrix(t *testing.T) {
	t.Parallel()

	logger, err := logging.New(logging.Config{Level: "debug"})
	if err != nil {
		t.Fatalf("logger init failed: %v", err)
	}

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	t.Run("local_without_token_allows", func(t *testing.T) {
		cfg := config.Default()
		handler := middleware.Auth(cfg, logger, false)(next)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
	})

	t.Run("remote_without_token_rejects", func(t *testing.T) {
		cfg := config.Default()
		cfg.Core.Bind = "0.0.0.0"
		handler := middleware.Auth(cfg, logger, true)(next)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
	})

	t.Run("token_with_valid_header_allows", func(t *testing.T) {
		cfg := config.Default()
		token := "secret"
		cfg.Core.Token = &token
		handler := middleware.Auth(cfg, logger, false)(next)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
		req.Header.Set("Authorization", "Bearer secret")
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
	})

	t.Run("token_with_invalid_header_rejects", func(t *testing.T) {
		cfg := config.Default()
		token := "secret"
		cfg.Core.Token = &token
		handler := middleware.Auth(cfg, logger, false)(next)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
		req.Header.Set("Authorization", "Bearer wrong")
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
	})
}
