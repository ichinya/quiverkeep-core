package apierrors

import (
	"encoding/json"
	"net/http"

	qerrors "github.com/ichinya/quiverkeep-core/internal/errors"
)

type Envelope struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

func Write(w http.ResponseWriter, err error) {
	code := qerrors.CodeOf(err)
	if code == "" {
		code = qerrors.CodeUnknown
	}

	status := toHTTPStatus(code)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(Envelope{
		Error:   string(code),
		Message: err.Error(),
	})
}

func toHTTPStatus(code qerrors.Code) int {
	switch code {
	case qerrors.CodeUnauthorized:
		return http.StatusUnauthorized
	case qerrors.CodeValidationFailed, qerrors.CodeConfigSchema:
		return http.StatusBadRequest
	case qerrors.CodePortInUse:
		return http.StatusConflict
	case qerrors.CodeProxyDisabled, qerrors.CodeProxyNotConfigured:
		return http.StatusServiceUnavailable
	case qerrors.CodeProxyUpstreamError:
		return http.StatusBadGateway
	case qerrors.CodeProxyTimeout:
		return http.StatusGatewayTimeout
	default:
		return http.StatusInternalServerError
	}
}
