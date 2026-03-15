package httpclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	qerrors "github.com/ichinya/quiverkeep-core/internal/errors"
	"github.com/ichinya/quiverkeep-core/internal/logging"
)

type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
	logger     *logging.Logger
}

func New(baseURL string, token string, logger *logging.Logger) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   strings.TrimSpace(token),
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		logger: logger,
	}
}

func (c *Client) GetJSON(ctx context.Context, path string, query url.Values, target any) error {
	fullURL := c.baseURL + path
	if len(query) > 0 {
		fullURL += "?" + query.Encode()
	}

	c.logger.Debug("http client request start",
		"component", "cli",
		"operation", "http_get",
		"url", fullURL,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		return qerrors.Wrap(qerrors.CodeConnectionRefused, "failed creating request", err)
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.logger.Error("http client request failed", "component", "cli", "operation", "http_get", "url", fullURL, "error", err)
		return qerrors.Wrap(qerrors.CodeConnectionRefused, "failed connecting to core", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusBadRequest {
		return c.errorFromResponse(resp, fullURL)
	}

	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		return qerrors.Wrap(qerrors.CodeConnectionRefused, "failed decoding response", err)
	}

	c.logger.Debug("http client request finish", "component", "cli", "operation", "http_get", "url", fullURL, "status", resp.StatusCode)
	return nil
}

type errorEnvelope struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

func (c *Client) errorFromResponse(resp *http.Response, fullURL string) error {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return qerrors.Wrap(qerrors.CodeConnectionRefused, "failed reading core error response", err)
	}

	status := resp.StatusCode
	code := codeFromStatus(status)
	message := fmt.Sprintf("core returned status %d", status)

	var envelope errorEnvelope
	if decodeErr := json.Unmarshal(body, &envelope); decodeErr == nil {
		if parsedCode := strings.TrimSpace(envelope.Error); parsedCode != "" {
			code = qerrors.Code(parsedCode)
		}
		if parsedMessage := normalizeEnvelopeMessage(envelope.Error, envelope.Message); parsedMessage != "" {
			message = parsedMessage
		}
	}

	c.logger.Warn("[FIX] http client received non-success response",
		"component", "cli",
		"operation", "http_get",
		"url", fullURL,
		"status", status,
		"error_code", code,
	)

	return qerrors.New(code, message)
}

func codeFromStatus(status int) qerrors.Code {
	switch status {
	case http.StatusBadRequest:
		return qerrors.CodeValidationFailed
	case http.StatusUnauthorized:
		return qerrors.CodeUnauthorized
	case http.StatusConflict:
		return qerrors.CodePortInUse
	default:
		if status >= http.StatusInternalServerError {
			return qerrors.CodeInternalServerError
		}
		return qerrors.CodeUnknown
	}
}

func normalizeEnvelopeMessage(rawCode string, rawMessage string) string {
	message := strings.TrimSpace(rawMessage)
	if message == "" {
		return ""
	}

	code := strings.TrimSpace(rawCode)
	prefix := code + ": "
	if code != "" && strings.HasPrefix(message, prefix) {
		return strings.TrimSpace(strings.TrimPrefix(message, prefix))
	}

	return message
}
