package httpx

import (
	"errors"
	"net/http"
)

type Error struct {
	Status  int
	Code    string
	Message string
	Cause   error
}

func (e *Error) Error() string {
	if e.Cause != nil {
		return e.Code + ": " + e.Cause.Error()
	}
	return e.Code + ": " + e.Message
}

func (e *Error) Unwrap() error { return e.Cause }

func BadRequest(code, message string) *Error {
	return &Error{Status: http.StatusBadRequest, Code: code, Message: message}
}

func Unauthorized(code, message string) *Error {
	return &Error{Status: http.StatusUnauthorized, Code: code, Message: message}
}

func Forbidden(code, message string) *Error {
	return &Error{Status: http.StatusForbidden, Code: code, Message: message}
}

func NotFound(code, message string) *Error {
	return &Error{Status: http.StatusNotFound, Code: code, Message: message}
}

func Conflict(code, message string) *Error {
	return &Error{Status: http.StatusConflict, Code: code, Message: message}
}

func Internal(err error) *Error {
	return &Error{
		Status:  http.StatusInternalServerError,
		Code:    "internal_error",
		Message: "요청을 처리하지 못했습니다",
		Cause:   err,
	}
}

func AsError(err error) *Error {
	var target *Error
	if errors.As(err, &target) {
		return target
	}
	return Internal(err)
}
