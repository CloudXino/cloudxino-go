package cloudxino

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// Error codes the API answers with (the "code" of an error body). Programs should branch on these,
// not on the message.
const (
	CodeBadRequest            = "bad_request"
	CodeValidation            = "validation_error"
	CodeUnauthenticated       = "unauthenticated"
	CodeForbidden             = "forbidden"
	CodeNotFound              = "not_found"
	CodeConflict              = "conflict"
	CodeAlreadyExists         = "already_exists"
	CodeInsufficientBalance   = "insufficient_balance"
	CodeQuotaExceeded         = "quota_exceeded"
	CodeRateLimited           = "rate_limited"
	CodeMaintenance           = "maintenance"
	CodeUnavailable           = "unavailable" // 503: the feature has nothing to give right now (e.g. no free reserved IP)
	CodeIdempotencyInProgress = "idempotency_in_progress"
	CodeIdempotencyKeyReused  = "idempotency_key_reused"
	CodeInternal              = "internal_error"
)

// APIError is an error answer from the CloudXino API.
type APIError struct {
	StatusCode int
	Code       string
	// Message is the human-readable detail; for validation errors, the field errors joined.
	Message string
	// Detail is the raw "detail" value (a string, or a list of field errors for 422).
	Detail     json.RawMessage
	RetryAfter time.Duration
	RequestID  string
	Method     string
	Path       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("cloudxino: %s %s: %d %s: %s", e.Method, e.Path, e.StatusCode, e.Code, e.Message)
}

// Retryable reports whether sending the same request again later may succeed.
func (e *APIError) Retryable() bool {
	switch e.Code {
	case CodeRateLimited, CodeMaintenance, CodeIdempotencyInProgress:
		return true
	case CodeUnavailable: // a 503 that a retry will not fix soon
		return false
	}
	switch e.StatusCode {
	case http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	return false
}

// IsNotFound reports whether err is a 404 from the API.
func IsNotFound(err error) bool { return hasStatus(err, http.StatusNotFound) }

// IsConflict reports whether err is a 409 from the API (e.g. an action the resource's state does not allow yet).
func IsConflict(err error) bool { return hasStatus(err, http.StatusConflict) }

// IsUnavailable reports whether err is the API's 503 "unavailable" answer: the feature cannot give
// what was asked right now (no free reserved IP, or reserved IPs are turned off).
func IsUnavailable(err error) bool { return ErrorCode(err) == CodeUnavailable }

// ErrorCode returns the API error code of err, or "" when err is not an *APIError.
func ErrorCode(err error) string {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.Code
	}
	return ""
}

func hasStatus(err error, status int) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == status
}

type errorBody struct {
	Code   string          `json:"code"`
	Detail json.RawMessage `json:"detail"`
	// older 429 bodies
	Message string `json:"message"`
}

type fieldError struct {
	Loc []any  `json:"loc"`
	Msg string `json:"msg"`
}

func parseError(resp *http.Response, body []byte) *APIError {
	e := &APIError{
		StatusCode: resp.StatusCode,
		RetryAfter: retryAfter(resp.Header),
		RequestID:  resp.Header.Get("X-Request-Id"),
		Method:     resp.Request.Method,
		Path:       resp.Request.URL.Path,
	}
	var b errorBody
	if json.Unmarshal(body, &b) == nil {
		e.Code = b.Code
		e.Detail = b.Detail
		var s string
		var fields []fieldError
		switch {
		case json.Unmarshal(b.Detail, &s) == nil:
			e.Message = s
		case json.Unmarshal(b.Detail, &fields) == nil && len(fields) > 0:
			for i, f := range fields {
				if i > 0 {
					e.Message += "; "
				}
				if n := len(f.Loc); n > 0 {
					e.Message += fmt.Sprint(f.Loc[n-1]) + ": "
				}
				e.Message += f.Msg
			}
		default:
			e.Message = b.Message
		}
	}
	if e.Message == "" {
		e.Message = http.StatusText(resp.StatusCode)
	}
	if e.Code == "" {
		e.Code = codeForStatus(resp.StatusCode)
	}
	return e
}

func codeForStatus(status int) string {
	switch status {
	case 400:
		return CodeBadRequest
	case 401:
		return CodeUnauthenticated
	case 402:
		return CodeInsufficientBalance
	case 403:
		return CodeForbidden
	case 404:
		return CodeNotFound
	case 409:
		return CodeConflict
	case 422:
		return CodeValidation
	case 429:
		return CodeRateLimited
	}
	if status >= 500 {
		return CodeInternal
	}
	return "client_error"
}
