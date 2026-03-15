package httpclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	qerrors "github.com/ichinya/quiverkeep-core/internal/errors"
	"github.com/ichinya/quiverkeep-core/internal/logging"
)

func TestGetJSONPreservesAPIErrorCodeAndMessage(t *testing.T) {
	t.Parallel()

	logger, err := logging.New(logging.Config{Level: "debug"})
	if err != nil {
		t.Fatalf("logger init failed: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"UNAUTHORIZED","message":"UNAUTHORIZED: invalid bearer token"}`))
	}))
	defer server.Close()

	client := New(server.URL, "", logger)

	var payload map[string]any
	err = client.GetJSON(context.Background(), "/api/v1/status", nil, &payload)
	if err == nil {
		t.Fatal("expected unauthorized error")
	}
	if qerrors.CodeOf(err) != qerrors.CodeUnauthorized {
		t.Fatalf("expected unauthorized code, got %v", qerrors.CodeOf(err))
	}
	if err.Error() != "UNAUTHORIZED: invalid bearer token" {
		t.Fatalf("unexpected error message: %q", err.Error())
	}
}

func TestGetJSONFallsBackToHTTPStatusWhenEnvelopeIsMissing(t *testing.T) {
	t.Parallel()

	logger, err := logging.New(logging.Config{Level: "debug"})
	if err != nil {
		t.Fatalf("logger init failed: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("bad request"))
	}))
	defer server.Close()

	client := New(server.URL, "", logger)

	var payload map[string]any
	err = client.GetJSON(context.Background(), "/api/v1/status", nil, &payload)
	if err == nil {
		t.Fatal("expected validation error")
	}
	if qerrors.CodeOf(err) != qerrors.CodeValidationFailed {
		t.Fatalf("expected validation failed code, got %v", qerrors.CodeOf(err))
	}
}
