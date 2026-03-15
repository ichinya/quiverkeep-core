package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ichinya/quiverkeep-core/internal/api/handlers"
	"github.com/ichinya/quiverkeep-core/internal/api/middleware"
	"github.com/ichinya/quiverkeep-core/internal/config"
	qerrors "github.com/ichinya/quiverkeep-core/internal/errors"
	"github.com/ichinya/quiverkeep-core/internal/logging"
	"github.com/ichinya/quiverkeep-core/internal/storage"
)

type Server struct {
	cfg      config.Config
	logger   *logging.Logger
	store    *storage.Store
	httpImpl *http.Server
}

func New(cfg config.Config, logger *logging.Logger, store *storage.Store) *Server {
	return &Server{
		cfg:    cfg,
		logger: logger,
		store:  store,
	}
}

func (s *Server) Run(ctx context.Context) error {
	mux := http.NewServeMux()
	api := handlers.New(s.store, s.cfg, s.logger)
	api.Register(mux)

	handler := middleware.RequestID(mux)
	handler = middleware.Logging(s.logger)(handler)
	handler = middleware.Auth(s.cfg, s.logger, isRemoteMode(s.cfg))(handler)

	bind := strings.TrimSpace(s.cfg.Core.Bind)
	if bind == "" {
		bind = "127.0.0.1"
	}
	port := s.cfg.Core.Port
	if port <= 0 {
		port = 8765
	}

	address := fmt.Sprintf("%s:%d", bind, port)
	s.httpImpl = &http.Server{
		Addr:              address,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	s.logger.Info("core http server start",
		"component", "api",
		"operation", "serve",
		"address", address,
		"remote_mode", isRemoteMode(s.cfg),
	)

	errCh := make(chan error, 1)
	go func() {
		if err := s.httpImpl.ListenAndServe(); err != nil {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		s.logger.Info("core http server shutdown requested", "component", "api", "operation", "shutdown")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := s.httpImpl.Shutdown(shutdownCtx); err != nil {
			return qerrors.Wrap(qerrors.CodeInternalServerError, "failed graceful shutdown", err)
		}
		return nil
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		if strings.Contains(strings.ToLower(err.Error()), "bind") || strings.Contains(strings.ToLower(err.Error()), "address already in use") {
			return qerrors.Wrap(qerrors.CodePortInUse, "port is already in use", err)
		}
		return qerrors.Wrap(qerrors.CodeInternalServerError, "http server failed", err)
	}
}

func isRemoteMode(cfg config.Config) bool {
	bind := strings.TrimSpace(cfg.Core.Bind)
	if bind == "" {
		return false
	}
	switch bind {
	case "127.0.0.1", "localhost", "::1":
		return false
	default:
		return true
	}
}
