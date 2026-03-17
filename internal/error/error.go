// Package error provides error codes and error handling utilities for the iris application.
// It defines a set of error codes that can be used throughout the application for consistent error handling.
package error

import (
	"errors"
	"fmt"

	"iris/internal/constants"
)

// ErrorCode represents a specific error condition in the application.
type ErrorCode int

// Error codes for the iris application.
// These should be used instead of error strings for consistent error handling.
const (
	ErrUnsupportedContentType ErrorCode = iota + 1000
	ErrImageExceedsLimit
	ErrNotFound
	ErrInvalidInput
	ErrRequiredField
	ErrFailedToReadFile
	ErrURLRequired
	ErrImageRequired
	ErrAdminAPIDisabled
	ErrCrawlUnavailable
	ErrJobStoreUnavailable
	ErrInternalServer
)

// errorMap maps error codes to their default messages.
var errorMap = map[ErrorCode]string{
	ErrUnsupportedContentType: constants.ErrorMsgUnsupportedContent,
	ErrImageExceedsLimit:      constants.ErrorMsgImageExceeds,
	ErrNotFound:               constants.ErrorMsgNotFound,
	ErrInvalidInput:           constants.ErrorMsgInvalid,
	ErrRequiredField:          constants.ErrorMsgIsRequired,
	ErrFailedToReadFile:       constants.ErrorMsgFailedToReadFile,
	ErrURLRequired:            constants.ErrorMsgURLRequired,
	ErrImageRequired:          constants.ErrorMsgImageRequired,
	ErrAdminAPIDisabled:       constants.MessageAdminAPIDisabled,
	ErrCrawlUnavailable:       constants.MessageCrawlServiceUnavailable,
	ErrJobStoreUnavailable:    constants.MessageJobStoreUnavailable,
	ErrInternalServer:         "internal server error",
}

// ErrorWith wraps the ErrorCode with another error.
// If err is nil, it returns the ErrorCode as an error.
func (c ErrorCode) ErrorWith(err error) error {
	if err != nil {
		return fmt.Errorf("%w: %v", c, err)
	}
	return c
}

// Error returns the ErrorCode as an error.
// This method satisfies the error interface.
func (c ErrorCode) Error() string {
	if msg, ok := errorMap[c]; ok {
		return msg
	}
	return "unknown error"
}

// Is reports whether err is of the target error code.
// This allows error checking using errors.Is().
func (c ErrorCode) Is(err error) bool {
	var target ErrorCode
	return errors.As(err, &target) && target == c
}
