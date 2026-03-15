package httpclient

import (
	"context"
	"encoding/json"
	"fmt"
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
		return qerrors.New(qerrors.CodeCoreNotRunning, fmt.Sprintf("core returned status %d", resp.StatusCode))
	}

	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		return qerrors.Wrap(qerrors.CodeConnectionRefused, "failed decoding response", err)
	}

	c.logger.Debug("http client request finish", "component", "cli", "operation", "http_get", "url", fullURL, "status", resp.StatusCode)
	return nil
}
