// Package core embeds a Tailscale ("tailnet") node directly into the current
// process using tailscale.com/tsnet. It requires no root privileges, no system
// VPN interface and no separately installed Tailscale client: all traffic that
// goes through the SDK travels through the tailnet from inside this process.
//
// The control plane is the official Tailscale coordination server
// (https://controlplane.tailscale.com) unless Config.ControlURL overrides it.
package core

import (
	"errors"
	"fmt"
)

// ErrorCode is a stable, language-agnostic error classification. Bindings
// (JNI, Swift, .NET) map it onto platform-native error types instead of
// relying on Go error strings.
type ErrorCode string

const (
	// ErrCodeInvalidArgument means the caller passed a bad value.
	ErrCodeInvalidArgument ErrorCode = "invalid_argument"
	// ErrCodeNotStarted means the node has not been started yet.
	ErrCodeNotStarted ErrorCode = "not_started"
	// ErrCodeTimeout means an operation exceeded its deadline or context.
	ErrCodeTimeout ErrorCode = "timeout"
	// ErrCodeLoginRequired means the node needs (interactive) authorization,
	// or is waiting for an admin to approve the machine.
	ErrCodeLoginRequired ErrorCode = "login_required"
	// ErrCodeClosed means the node was closed and can no longer be used.
	ErrCodeClosed ErrorCode = "closed"
	// ErrCodeBackend means the tailnet backend reported a failure.
	ErrCodeBackend ErrorCode = "backend"
	// ErrCodeUnknown is used for errors created outside this package.
	ErrCodeUnknown ErrorCode = "unknown"
)

// Error is the error type returned by this SDK. It always carries an
// ErrorCode so that non-Go callers can act on it.
type Error struct {
	Code ErrorCode
	Op   string
	Msg  string
	Err  error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	switch {
	case e.Op != "" && e.Err != nil:
		return fmt.Sprintf("tailnet: %s: %s: %v", e.Op, e.Msg, e.Err)
	case e.Op != "":
		return fmt.Sprintf("tailnet: %s: %s", e.Op, e.Msg)
	case e.Err != nil:
		return fmt.Sprintf("tailnet: %s: %v", e.Msg, e.Err)
	default:
		return "tailnet: " + e.Msg
	}
}

// Unwrap allows errors.Is/errors.As to inspect the wrapped cause.
func (e *Error) Unwrap() error { return e.Err }

func newError(code ErrorCode, op, msg string, err error) *Error {
	return &Error{Code: code, Op: op, Msg: msg, Err: err}
}

// Code returns the ErrorCode carried by err, or ErrCodeUnknown for any error
// that was not produced by this SDK. It returns the empty string for nil.
func Code(err error) ErrorCode {
	if err == nil {
		return ""
	}
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ErrCodeUnknown
}

// IsCode reports whether err carries the given ErrorCode.
func IsCode(err error, code ErrorCode) bool { return Code(err) == code }
