package handlers

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	apierrors "github.com/ichinya/quiverkeep-core/internal/api/errors"
	"github.com/ichinya/quiverkeep-core/internal/api/middleware"
	"github.com/ichinya/quiverkeep-core/internal/config"
	"github.com/ichinya/quiverkeep-core/internal/domain"
	qerrors "github.com/ichinya/quiverkeep-core/internal/errors"
	"github.com/ichinya/quiverkeep-core/internal/logging"
	proxyapi "github.com/ichinya/quiverkeep-core/internal/proxy/api"
	"github.com/ichinya/quiverkeep-core/internal/storage"
	"github.com/ichinya/quiverkeep-core/internal/version"
)

const maxProxyBodyBytes = 2 * 1024 * 1024

type API struct {
	store          *storage.Store
	cfg            config.Config
	startedAt      time.Time
	logger         *logging.Logger
	anthropicProxy *proxyapi.AnthropicProxy
}

func New(store *storage.Store, cfg config.Config, logger *logging.Logger) *API {
	return &API{
		store:          store,
		cfg:            cfg,
		startedAt:      time.Now().UTC(),
		logger:         logger,
		anthropicProxy: proxyapi.NewAnthropicProxy(cfg, logger, store),
	}
}

func (a *API) Register(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/status", a.status)
	mux.HandleFunc("/api/v1/usage", a.usage)
	mux.HandleFunc("/api/v1/limits", a.limits)
	mux.HandleFunc("/api/v1/subscriptions", a.subscriptions)
	mux.HandleFunc("/api/v1/providers", a.providers)
	mux.HandleFunc("/api/v1/proxy/anthropic/messages", a.proxyAnthropicMessages)
	mux.HandleFunc("/api/v1/proxy/status", a.proxyStatus)
}

func (a *API) status(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		apierrors.Write(w, qerrors.New(qerrors.CodeValidationFailed, "method not allowed"))
		return
	}

	response := map[string]any{
		"status":         "ok",
		"version":        version.BuildVersion,
		"uptime_seconds": int(time.Since(a.startedAt).Seconds()),
	}
	writeJSON(w, http.StatusOK, response)
}

func (a *API) usage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		apierrors.Write(w, qerrors.New(qerrors.CodeValidationFailed, "method not allowed"))
		return
	}

	filter, err := parseUsageFilter(r)
	if err != nil {
		apierrors.Write(w, qerrors.Wrap(qerrors.CodeValidationFailed, "invalid usage filter", err))
		return
	}

	items, err := a.store.ListUsage(r.Context(), filter)
	if err != nil {
		a.logger.Error("usage query failed", "component", "api", "operation", "usage", "error", err)
		apierrors.Write(w, err)
		return
	}

	total, err := a.store.UsageSummary(r.Context(), filter)
	if err != nil {
		a.logger.Error("usage summary failed", "component", "api", "operation", "usage", "error", err)
		apierrors.Write(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"items": items,
		"total": total,
	})
}

func (a *API) limits(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		apierrors.Write(w, qerrors.New(qerrors.CodeValidationFailed, "method not allowed"))
		return
	}

	limits, err := a.store.Limits(r.Context())
	if err != nil {
		a.logger.Error("limits query failed", "component", "api", "operation", "limits", "error", err)
		apierrors.Write(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": limits})
}

func (a *API) subscriptions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		apierrors.Write(w, qerrors.New(qerrors.CodeValidationFailed, "method not allowed"))
		return
	}

	items, err := a.store.ListSubscriptions(r.Context())
	if err != nil {
		a.logger.Error("subscriptions query failed", "component", "api", "operation", "subscriptions", "error", err)
		apierrors.Write(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (a *API) providers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		apierrors.Write(w, qerrors.New(qerrors.CodeValidationFailed, "method not allowed"))
		return
	}

	providers := []domain.ProviderStatus{
		{ID: "openai", Name: "OpenAI", Configured: strings.TrimSpace(a.cfg.Providers.OpenAI.Key) != ""},
		{ID: "anthropic", Name: "Anthropic", Configured: strings.TrimSpace(a.cfg.Providers.Anthropic.Key) != ""},
		{ID: "copilot", Name: "Copilot", Configured: strings.TrimSpace(a.cfg.Providers.Copilot.Token) != ""},
	}

	writeJSON(w, http.StatusOK, map[string]any{"items": providers})
}

func (a *API) proxyAnthropicMessages(w http.ResponseWriter, r *http.Request) {
	requestID := middleware.RequestIDFromContext(r.Context())
	a.logger.Info("proxy endpoint invocation",
		"component", "api",
		"operation", "proxy_forward",
		"provider", "anthropic",
		"request_id", requestID,
		"method", r.Method,
		"path", r.URL.Path,
	)

	if r.Method != http.MethodPost {
		a.logger.Warn("proxy method not allowed",
			"component", "api",
			"operation", "proxy_forward",
			"provider", "anthropic",
			"request_id", requestID,
			"method", r.Method,
			"path", r.URL.Path,
		)
		apierrors.Write(w, qerrors.New(qerrors.CodeValidationFailed, "method not allowed"))
		return
	}

	contentType := strings.TrimSpace(strings.ToLower(r.Header.Get("Content-Type")))
	if contentType != "" && !strings.HasPrefix(contentType, "application/json") {
		a.logger.Warn("proxy content-type validation failed",
			"component", "api",
			"operation", "proxy_forward",
			"provider", "anthropic",
			"request_id", requestID,
			"content_type", contentType,
		)
		apierrors.Write(w, qerrors.New(qerrors.CodeValidationFailed, "content-type must be application/json"))
		return
	}

	limited := io.LimitReader(r.Body, maxProxyBodyBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		a.logger.Warn("proxy request body read failed",
			"component", "api",
			"operation", "proxy_forward",
			"provider", "anthropic",
			"request_id", requestID,
		)
		apierrors.Write(w, qerrors.Wrap(qerrors.CodeValidationFailed, "failed to read request body", err))
		return
	}
	if len(body) == 0 {
		a.logger.Warn("proxy request body is empty",
			"component", "api",
			"operation", "proxy_forward",
			"provider", "anthropic",
			"request_id", requestID,
		)
		apierrors.Write(w, qerrors.New(qerrors.CodeValidationFailed, "request body is required"))
		return
	}
	if len(body) > maxProxyBodyBytes {
		a.logger.Warn("proxy request body exceeds size limit",
			"component", "api",
			"operation", "proxy_forward",
			"provider", "anthropic",
			"request_id", requestID,
			"body_size", len(body),
			"max_body_size", maxProxyBodyBytes,
		)
		apierrors.Write(w, qerrors.New(qerrors.CodeValidationFailed, "request body exceeds size limit"))
		return
	}

	response, err := a.anthropicProxy.Forward(r.Context(), proxyapi.ForwardRequest{
		Payload:       body,
		AnthropicBeta: strings.TrimSpace(r.Header.Get("Anthropic-Beta")),
		RequestID:     requestID,
	})
	if err != nil {
		a.logger.Error("proxy forward failed",
			"component", "api",
			"operation", "proxy_forward",
			"provider", "anthropic",
			"request_id", requestID,
			"error", err,
		)
		apierrors.Write(w, err)
		return
	}

	for key, values := range response.Headers {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	if strings.TrimSpace(w.Header().Get("Content-Type")) == "" {
		w.Header().Set("Content-Type", "application/json")
	}
	w.WriteHeader(response.StatusCode)
	_, _ = w.Write(response.Body)

	a.logger.Info("proxy forward response passthrough",
		"component", "api",
		"operation", "proxy_forward",
		"provider", "anthropic",
		"request_id", requestID,
		"status", response.StatusCode,
	)
}

func (a *API) proxyStatus(w http.ResponseWriter, r *http.Request) {
	requestID := middleware.RequestIDFromContext(r.Context())
	if r.Method != http.MethodGet {
		a.logger.Warn("proxy status method not allowed",
			"component", "api",
			"operation", "proxy_status",
			"request_id", requestID,
			"method", r.Method,
			"path", r.URL.Path,
		)
		apierrors.Write(w, qerrors.New(qerrors.CodeValidationFailed, "method not allowed"))
		return
	}

	a.logger.Debug("proxy diagnostics endpoint read",
		"component", "api",
		"operation", "proxy_status",
		"request_id", requestID,
		"path", r.URL.Path,
	)

	status := a.anthropicProxy.Status()
	writeJSON(w, http.StatusOK, map[string]any{
		"items": []proxyapi.Status{status},
	})
}

func parseUsageFilter(r *http.Request) (domain.UsageFilter, error) {
	filter := domain.UsageFilter{
		Service: strings.TrimSpace(r.URL.Query().Get("service")),
	}

	fromRaw := strings.TrimSpace(r.URL.Query().Get("from"))
	if fromRaw != "" {
		parsed, err := time.Parse(time.RFC3339, fromRaw)
		if err != nil {
			return domain.UsageFilter{}, err
		}
		filter.From = &parsed
	}

	toRaw := strings.TrimSpace(r.URL.Query().Get("to"))
	if toRaw != "" {
		parsed, err := time.Parse(time.RFC3339, toRaw)
		if err != nil {
			return domain.UsageFilter{}, err
		}
		filter.To = &parsed
	}

	if limitRaw := strings.TrimSpace(r.URL.Query().Get("limit")); limitRaw != "" {
		if _, err := strconv.Atoi(limitRaw); err != nil {
			return domain.UsageFilter{}, err
		}
	}

	return filter, nil
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
