// Package domain holds product objects and errors. It imports only the
// standard library so app, api and store can share it without framework ties.
package domain

import (
	"errors"
	"fmt"
)

// Code is the machine-readable error code of the public wire contract.
type Code string

const (
	CodeInvalidRequest      Code = "invalid_request"
	CodeUnauthorized        Code = "unauthorized"
	CodeForbidden           Code = "forbidden"
	CodeNotFound            Code = "not_found"
	CodeConflict            Code = "conflict"
	CodeIdempotencyConflict Code = "idempotency_conflict"
	CodeRevisionConflict    Code = "revision_conflict"
	CodeCapabilityMissing   Code = "capability_missing"
	CodeEngineUnavailable   Code = "engine_unavailable"
	CodeInternal            Code = "internal"
)

// Error is a structured product error. MessageKey addresses an i18n entry;
// Params carries non-secret interpolation values. Err is never serialized.
type Error struct {
	Code       Code
	MessageKey string
	Params     map[string]any
	Err        error
}

func (e *Error) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Code, e.Err)
	}
	return string(e.Code)
}

func (e *Error) Unwrap() error { return e.Err }

func newError(code Code, key string, params map[string]any, err error) *Error {
	if params == nil {
		params = map[string]any{}
	}
	return &Error{Code: code, MessageKey: key, Params: params, Err: err}
}

// InvalidRequest reports bad input that the caller can fix.
func InvalidRequest(key string, params map[string]any) *Error {
	return newError(CodeInvalidRequest, key, params, nil)
}

// Unauthorized reports a missing or invalid credential.
func Unauthorized(key string) *Error {
	return newError(CodeUnauthorized, key, nil, nil)
}

// Forbidden reports an authenticated caller outside its permission scope.
func Forbidden(key string, params map[string]any) *Error {
	return newError(CodeForbidden, key, params, nil)
}

// NotFound reports a missing resource.
func NotFound(key string, params map[string]any) *Error {
	return newError(CodeNotFound, key, params, nil)
}

// Conflict reports lifecycle or uniqueness conflicts (HTTP 409).
func Conflict(key string, params map[string]any) *Error {
	return newError(CodeConflict, key, params, nil)
}

// RevisionConflict reports a stale expectedRevision (HTTP 409).
func RevisionConflict(key string, params map[string]any) *Error {
	return newError(CodeRevisionConflict, key, params, nil)
}

// IdempotencyConflict reports the same requestId reused with a different body.
func IdempotencyConflict(key string, params map[string]any) *Error {
	return newError(CodeIdempotencyConflict, key, params, nil)
}

// CapabilityMissing reports that the current engine/platform cannot express a
// required operation (HTTP 422). The product must not silently downgrade.
func CapabilityMissing(key string, params map[string]any, err error) *Error {
	return newError(CodeCapabilityMissing, key, params, err)
}

// EngineUnavailable reports that the external agent engine cannot serve the
// request right now (HTTP 503).
func EngineUnavailable(key string, params map[string]any, err error) *Error {
	return newError(CodeEngineUnavailable, key, params, err)
}

// Internal wraps unexpected failures; details stay in logs, not on the wire.
func Internal(err error) *Error {
	return newError(CodeInternal, "error.internal", nil, err)
}

// AsError normalizes any error into a *Error, defaulting to internal.
func AsError(err error) *Error {
	if err == nil {
		return nil
	}
	var de *Error
	if errors.As(err, &de) {
		return de
	}
	return Internal(err)
}

// HTTPStatus maps a wire code onto the HTTP status required by the contract.
func (e *Error) HTTPStatus() int {
	switch e.Code {
	case CodeInvalidRequest:
		return 400
	case CodeUnauthorized:
		return 401
	case CodeForbidden:
		return 403
	case CodeNotFound:
		return 404
	case CodeConflict, CodeIdempotencyConflict, CodeRevisionConflict:
		return 409
	case CodeCapabilityMissing:
		return 422
	case CodeEngineUnavailable:
		return 503
	default:
		return 500
	}
}
