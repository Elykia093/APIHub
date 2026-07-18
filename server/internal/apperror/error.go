package apperror

import (
	"context"
	"errors"
	"net/http"
)

const (
	AuthRequired             = "AUTH_REQUIRED"
	BadRequest               = "BAD_REQUEST"
	PayloadTooLarge          = "PAYLOAD_TOO_LARGE"
	UnsupportedMediaType     = "UNSUPPORTED_MEDIA_TYPE"
	RateLimited              = "RATE_LIMITED"
	ValidationError          = "VALIDATION_ERROR"
	NotFound                 = "NOT_FOUND"
	Conflict                 = "CONFLICT"
	UpstreamTimeout          = "UPSTREAM_TIMEOUT"
	UpstreamRejected         = "UPSTREAM_REJECTED"
	UpstreamResponseTooLarge = "UPSTREAM_RESPONSE_TOO_LARGE"
	UpstreamRedirectBlocked  = "UPSTREAM_REDIRECT_BLOCKED"
	ManualActionRequired     = "MANUAL_ACTION_REQUIRED"
	SiteURLBlocked           = "SITE_URL_BLOCKED"
	InternalError            = "INTERNAL_ERROR"
)

type Error struct {
	Status    int
	Code      string
	Message   string
	Retryable bool
	Cause     error
}

func (e *Error) Error() string { return e.Message }
func (e *Error) Unwrap() error { return e.Cause }

func New(status int, code, message string, retryable bool) *Error {
	return &Error{Status: status, Code: code, Message: message, Retryable: retryable}
}

func Wrap(status int, code, message string, retryable bool, cause error) *Error {
	return &Error{Status: status, Code: code, Message: message, Retryable: retryable, Cause: cause}
}

func As(err error) *Error {
	var appErr *Error
	if errors.As(err, &appErr) {
		return appErr
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return Wrap(http.StatusGatewayTimeout, UpstreamTimeout, "Upstream request timed out", true, err)
	}
	return Wrap(http.StatusInternalServerError, InternalError, "Internal server error", false, err)
}
