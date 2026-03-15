package qerrors

import "errors"

type Code string

const (
	CodeUnknown             Code = "UNKNOWN_ERROR"
	CodeUnauthorized        Code = "UNAUTHORIZED"
	CodeConfigParse         Code = "CONFIG_PARSE_ERROR"
	CodeConfigPermission    Code = "CONFIG_PERMISSION_ERROR"
	CodeConfigSchema        Code = "CONFIG_SCHEMA_ERROR"
	CodeCoreNotRunning      Code = "CORE_NOT_RUNNING"
	CodeConnectionRefused   Code = "CONNECTION_REFUSED"
	CodePortInUse           Code = "PORT_IN_USE"
	CodeStorageOpen         Code = "STORAGE_OPEN_ERROR"
	CodeStorageMigration    Code = "STORAGE_MIGRATION_ERROR"
	CodeStorageQuery        Code = "STORAGE_QUERY_ERROR"
	CodeStorageLock         Code = "STORAGE_LOCK_ERROR"
	CodeStoragePermission   Code = "STORAGE_PERMISSION_ERROR"
	CodeStorageCorrupt      Code = "STORAGE_CORRUPT_ERROR"
	CodeValidationFailed    Code = "VALIDATION_FAILED"
	CodeInternalServerError Code = "INTERNAL_SERVER_ERROR"
)

type AppError struct {
	Code    Code
	Message string
	Err     error
}

func (e *AppError) Error() string {
	if e.Err == nil {
		return string(e.Code) + ": " + e.Message
	}
	return string(e.Code) + ": " + e.Message + ": " + e.Err.Error()
}

func (e *AppError) Unwrap() error {
	return e.Err
}

func New(code Code, message string) *AppError {
	return &AppError{
		Code:    code,
		Message: message,
	}
}

func Wrap(code Code, message string, err error) *AppError {
	return &AppError{
		Code:    code,
		Message: message,
		Err:     err,
	}
}

func CodeOf(err error) Code {
	if err == nil {
		return ""
	}

	var appErr *AppError
	if errors.As(err, &appErr) {
		return appErr.Code
	}

	return CodeUnknown
}
