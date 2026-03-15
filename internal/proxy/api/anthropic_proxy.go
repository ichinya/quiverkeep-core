package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/ichinya/quiverkeep-core/internal/config"
	"github.com/ichinya/quiverkeep-core/internal/domain"
	qerrors "github.com/ichinya/quiverkeep-core/internal/errors"
	"github.com/ichinya/quiverkeep-core/internal/logging"
)

const (
	anthropicMessagesPath   = "/v1/messages"
	defaultAnthropicBaseURL = "https://api.anthropic.com"
	defaultAnthropicVersion = "2023-06-01"
	maxUpstreamBodyBytes    = 8 * 1024 * 1024
)

type UsageWriter interface {
	InsertUsage(ctx context.Context, usage domain.UsageRecord) error
}

type ForwardRequest struct {
	Payload       []byte
	AnthropicBeta string
	RequestID     string
}

type ForwardResponse struct {
	StatusCode int
	Body       []byte
	Headers    http.Header
}

type Status struct {
	Provider         string     `json:"provider"`
	Enabled          bool       `json:"enabled"`
	Configured       bool       `json:"configured"`
	LastAttemptAt    *time.Time `json:"last_attempt_at,omitempty"`
	LastSuccessAt    *time.Time `json:"last_success_at,omitempty"`
	LastErrorCode    string     `json:"last_error_code,omitempty"`
	LastErrorMessage string     `json:"last_error_message,omitempty"`
	LastUpstreamCode *int       `json:"last_upstream_code,omitempty"`
}

type AnthropicProxy struct {
	cfg    config.Config
	logger *logging.Logger
	store  UsageWriter
	client *http.Client

	mu     sync.RWMutex
	status Status
}

func NewAnthropicProxy(cfg config.Config, logger *logging.Logger, store UsageWriter) *AnthropicProxy {
	timeoutSeconds := cfg.Proxy.Anthropic.TimeoutSeconds
	if timeoutSeconds <= 0 {
		timeoutSeconds = 30
	}

	return &AnthropicProxy{
		cfg:    cfg,
		logger: logger,
		store:  store,
		client: &http.Client{Timeout: time.Duration(timeoutSeconds) * time.Second},
		status: Status{
			Provider:   "anthropic",
			Enabled:    cfg.Proxy.Enabled,
			Configured: strings.TrimSpace(cfg.Providers.Anthropic.Key) != "",
		},
	}
}

func (p *AnthropicProxy) Forward(ctx context.Context, req ForwardRequest) (ForwardResponse, error) {
	modelHint := modelHintFromPayload(req.Payload)
	p.markAttempt(req.RequestID, modelHint)

	if !p.cfg.Proxy.Enabled {
		err := qerrors.New(qerrors.CodeProxyDisabled, "proxy mode is disabled")
		p.markFailure(req.RequestID, err, nil)
		return ForwardResponse{}, err
	}

	apiKey := strings.TrimSpace(p.cfg.Providers.Anthropic.Key)
	if apiKey == "" {
		err := qerrors.New(qerrors.CodeProxyNotConfigured, "anthropic provider key is not configured")
		p.markFailure(req.RequestID, err, nil)
		return ForwardResponse{}, err
	}

	baseURL := strings.TrimSpace(p.cfg.Proxy.Anthropic.BaseURL)
	if baseURL == "" {
		baseURL = defaultAnthropicBaseURL
	}
	endpoint := strings.TrimRight(baseURL, "/") + anthropicMessagesPath
	logEndpoint, redacted := sanitizeEndpointForLogs(endpoint)
	p.logger.Debug("proxy endpoint resolved",
		"component", "proxy",
		"operation", "proxy_forward",
		"provider", "anthropic",
		"request_id", req.RequestID,
		"endpoint", logEndpoint,
		"endpoint_redacted", redacted,
	)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(req.Payload))
	if err != nil {
		proxyErr := qerrors.New(qerrors.CodeProxyUpstreamError, "failed to create upstream request")
		p.markFailure(req.RequestID, proxyErr, nil)
		p.logger.Error("proxy upstream request creation failed",
			"component", "proxy",
			"operation", "proxy_forward",
			"provider", "anthropic",
			"request_id", req.RequestID,
			"error_code", qerrors.CodeOf(proxyErr),
			"retry_decision", "no_retry",
		)
		return ForwardResponse{}, proxyErr
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-API-Key", apiKey)
	version := strings.TrimSpace(p.cfg.Proxy.Anthropic.Version)
	if version == "" {
		version = defaultAnthropicVersion
	}
	httpReq.Header.Set("Anthropic-Version", version)
	if beta := strings.TrimSpace(req.AnthropicBeta); beta != "" {
		httpReq.Header.Set("Anthropic-Beta", beta)
	}

	p.logger.Debug("proxy forward start",
		"component", "proxy",
		"operation", "proxy_forward",
		"provider", "anthropic",
		"request_id", req.RequestID,
		"model_hint", modelHint,
	)

	started := time.Now()
	p.logger.Info("proxy upstream call start",
		"component", "proxy",
		"operation", "proxy_forward",
		"provider", "anthropic",
		"request_id", req.RequestID,
		"endpoint", logEndpoint,
	)

	upstreamResp, err := p.client.Do(httpReq)
	if err != nil {
		duration := time.Since(started).Milliseconds()
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			timeoutErr := qerrors.New(qerrors.CodeProxyTimeout, "anthropic upstream request timed out")
			p.markFailure(req.RequestID, timeoutErr, nil)
			p.logger.Error("proxy upstream call failed",
				"component", "proxy",
				"operation", "proxy_forward",
				"provider", "anthropic",
				"request_id", req.RequestID,
				"error_code", qerrors.CodeOf(timeoutErr),
				"error_class", classifyForwardError(err),
				"duration_ms", duration,
				"retry_decision", "no_retry",
			)
			return ForwardResponse{}, timeoutErr
		}
		proxyErr := qerrors.New(qerrors.CodeProxyUpstreamError, "anthropic upstream request failed")
		p.markFailure(req.RequestID, proxyErr, nil)
		p.logger.Error("proxy upstream call failed",
			"component", "proxy",
			"operation", "proxy_forward",
			"provider", "anthropic",
			"request_id", req.RequestID,
			"error_code", qerrors.CodeOf(proxyErr),
			"error_class", classifyForwardError(err),
			"duration_ms", duration,
			"retry_decision", "no_retry",
		)
		return ForwardResponse{}, proxyErr
	}
	defer upstreamResp.Body.Close()

	limitedBody := io.LimitReader(upstreamResp.Body, maxUpstreamBodyBytes+1)
	body, err := io.ReadAll(limitedBody)
	if err != nil {
		proxyErr := qerrors.New(qerrors.CodeProxyUpstreamError, "failed reading anthropic upstream response")
		p.markFailure(req.RequestID, proxyErr, &upstreamResp.StatusCode)
		p.logger.Error("proxy upstream response read failed",
			"component", "proxy",
			"operation", "proxy_forward",
			"provider", "anthropic",
			"request_id", req.RequestID,
			"error_code", qerrors.CodeOf(proxyErr),
			"status", upstreamResp.StatusCode,
			"error_class", classifyForwardError(err),
			"retry_decision", "no_retry",
		)
		return ForwardResponse{}, proxyErr
	}
	if len(body) > maxUpstreamBodyBytes {
		proxyErr := qerrors.New(qerrors.CodeProxyUpstreamError, "anthropic upstream response exceeds size limit")
		p.markFailure(req.RequestID, proxyErr, &upstreamResp.StatusCode)
		p.logger.Error("proxy upstream response exceeded size limit",
			"component", "proxy",
			"operation", "proxy_forward",
			"provider", "anthropic",
			"request_id", req.RequestID,
			"error_code", qerrors.CodeOf(proxyErr),
			"status", upstreamResp.StatusCode,
			"response_size", len(body),
			"max_response_size", maxUpstreamBodyBytes,
			"retry_decision", "no_retry",
		)
		return ForwardResponse{}, proxyErr
	}

	duration := time.Since(started).Milliseconds()
	p.logger.Info("proxy upstream call finish",
		"component", "proxy",
		"operation", "proxy_forward",
		"provider", "anthropic",
		"request_id", req.RequestID,
		"status", upstreamResp.StatusCode,
		"duration_ms", duration,
	)

	if upstreamResp.StatusCode < http.StatusOK || upstreamResp.StatusCode >= http.StatusMultipleChoices {
		proxyErr := proxyErrorFromUpstreamStatus(upstreamResp.StatusCode)
		p.markFailure(req.RequestID, proxyErr, &upstreamResp.StatusCode)
		p.logger.Error("proxy upstream returned non-success status",
			"component", "proxy",
			"operation", "proxy_forward",
			"provider", "anthropic",
			"request_id", req.RequestID,
			"status", upstreamResp.StatusCode,
			"duration_ms", duration,
			"error_code", qerrors.CodeOf(proxyErr),
			"retry_decision", "no_retry",
		)
		return ForwardResponse{}, proxyErr
	}

	p.markSuccess(req.RequestID, upstreamResp.StatusCode)
	p.trackUsage(ctx, body, upstreamResp.StatusCode, req.RequestID)

	return ForwardResponse{
		StatusCode: upstreamResp.StatusCode,
		Body:       body,
		Headers:    copyProxyHeaders(upstreamResp.Header),
	}, nil
}

func (p *AnthropicProxy) Status() Status {
	p.mu.RLock()
	status := p.status
	p.mu.RUnlock()

	status.Enabled = p.cfg.Proxy.Enabled
	status.Configured = strings.TrimSpace(p.cfg.Providers.Anthropic.Key) != ""
	if status.LastAttemptAt == nil && (status.LastSuccessAt != nil || status.LastErrorCode != "") {
		p.logger.Warn("proxy diagnostics state is inconsistent",
			"component", "proxy",
			"operation", "proxy_status",
			"provider", "anthropic",
		)
	}
	p.logger.Debug("proxy diagnostics read",
		"component", "proxy",
		"operation", "proxy_status",
		"provider", "anthropic",
		"enabled", status.Enabled,
		"configured", status.Configured,
		"last_error_code", status.LastErrorCode,
		"last_upstream_code", status.LastUpstreamCode,
	)
	return status
}

func (p *AnthropicProxy) trackUsage(ctx context.Context, body []byte, statusCode int, requestID string) {
	if p.store == nil {
		return
	}
	if statusCode < http.StatusOK || statusCode >= http.StatusMultipleChoices {
		return
	}

	var response struct {
		Model string `json:"model"`
		Usage struct {
			InputTokens  int64 `json:"input_tokens"`
			OutputTokens int64 `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		p.logger.Warn("proxy usage parse failed",
			"component", "proxy",
			"operation", "proxy_usage",
			"provider", "anthropic",
			"request_id", requestID,
			"error_class", classifyForwardError(err),
		)
		return
	}

	if response.Usage.InputTokens == 0 && response.Usage.OutputTokens == 0 {
		p.logger.Warn("proxy usage missing token fields",
			"component", "proxy",
			"operation", "proxy_usage",
			"provider", "anthropic",
			"request_id", requestID,
			"error_class", "missing_usage",
		)
		return
	}

	model := strings.TrimSpace(response.Model)
	if model == "" {
		model = "unknown"
	}

	p.logger.Debug("proxy usage extracted",
		"component", "proxy",
		"operation", "proxy_usage",
		"provider", "anthropic",
		"request_id", requestID,
		"tokens_in", response.Usage.InputTokens,
		"tokens_out", response.Usage.OutputTokens,
		"cost", 0,
		"model", model,
	)

	err := p.store.InsertUsage(ctx, domain.UsageRecord{
		Service:   "anthropic",
		Model:     model,
		TokensIn:  response.Usage.InputTokens,
		TokensOut: response.Usage.OutputTokens,
		Cost:      0,
		CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		p.logger.Warn("proxy usage persist failed",
			"component", "proxy",
			"operation", "proxy_usage",
			"provider", "anthropic",
			"request_id", requestID,
			"error_class", classifyForwardError(err),
		)
		return
	}

	p.logger.Info("proxy usage persisted",
		"component", "proxy",
		"operation", "proxy_usage",
		"provider", "anthropic",
		"request_id", requestID,
		"tokens_in", response.Usage.InputTokens,
		"tokens_out", response.Usage.OutputTokens,
		"model", model,
	)
}

func (p *AnthropicProxy) markAttempt(requestID string, modelHint string) {
	now := time.Now().UTC()
	p.mu.Lock()
	p.status.LastAttemptAt = &now
	p.mu.Unlock()

	p.logger.Info("proxy diagnostics state transition",
		"component", "proxy",
		"operation", "proxy_status",
		"provider", "anthropic",
		"request_id", requestID,
		"state", "attempt",
		"model_hint", modelHint,
	)
}

func (p *AnthropicProxy) markSuccess(requestID string, statusCode int) {
	now := time.Now().UTC()
	p.mu.Lock()
	p.status.LastSuccessAt = &now
	p.status.LastErrorCode = ""
	p.status.LastErrorMessage = ""
	p.status.LastUpstreamCode = &statusCode
	p.mu.Unlock()

	p.logger.Info("proxy diagnostics state transition",
		"component", "proxy",
		"operation", "proxy_status",
		"provider", "anthropic",
		"request_id", requestID,
		"state", "success",
		"last_upstream_code", statusCode,
	)
}

func (p *AnthropicProxy) markFailure(requestID string, err error, statusCode *int) {
	p.mu.Lock()
	p.status.LastErrorCode = string(qerrors.CodeOf(err))
	p.status.LastErrorMessage = statusMessageFromError(err)
	p.status.LastUpstreamCode = statusCode
	p.mu.Unlock()

	p.logger.Info("proxy diagnostics state transition",
		"component", "proxy",
		"operation", "proxy_status",
		"provider", "anthropic",
		"request_id", requestID,
		"state", "failure",
		"error_code", qerrors.CodeOf(err),
		"last_upstream_code", statusCode,
	)
}

func copyProxyHeaders(src http.Header) http.Header {
	dst := make(http.Header)
	for _, key := range []string{"Content-Type", "Anthropic-Request-Id", "X-Request-Id"} {
		values := src.Values(key)
		if len(values) == 0 {
			continue
		}
		for _, value := range values {
			dst.Add(key, value)
		}
	}
	return dst
}

func modelHintFromPayload(payload []byte) string {
	if len(payload) == 0 {
		return ""
	}

	var request struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(payload, &request); err != nil {
		return ""
	}
	return strings.TrimSpace(request.Model)
}

func sanitizeEndpointForLogs(rawEndpoint string) (string, bool) {
	parsed, err := url.Parse(rawEndpoint)
	if err != nil {
		return rawEndpoint, false
	}

	redacted := false
	if parsed.User != nil {
		parsed.User = url.User("redacted")
		redacted = true
	}
	if parsed.RawQuery != "" {
		parsed.RawQuery = ""
		redacted = true
	}
	if parsed.Fragment != "" {
		parsed.Fragment = ""
		redacted = true
	}

	return parsed.String(), redacted
}

func classifyForwardError(err error) string {
	if err == nil {
		return ""
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "timeout"
	}
	return "upstream_error"
}

func statusMessageFromError(err error) string {
	if err == nil {
		return ""
	}

	var appErr *qerrors.AppError
	if errors.As(err, &appErr) {
		return appErr.Message
	}
	return "proxy operation failed"
}

func proxyErrorFromUpstreamStatus(statusCode int) *qerrors.AppError {
	if statusCode >= http.StatusBadRequest && statusCode < http.StatusInternalServerError {
		return qerrors.New(qerrors.CodeValidationFailed, fmt.Sprintf("anthropic request rejected with status %d", statusCode))
	}
	return qerrors.New(qerrors.CodeProxyUpstreamError, fmt.Sprintf("anthropic upstream returned %d", statusCode))
}
