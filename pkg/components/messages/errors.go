package messages

import (
	hs_errors "github.com/trentnix/hyperserver/pkg/errors"
)

// BaseError aliases the shared HyperServer error wrapper.
type BaseError = hs_errors.BaseError

// ErrRetrievingContentMessagesData reports a failure converting stored message data.
type ErrRetrievingContentMessagesData struct {
	*BaseError
}

// NewErrConvertingContentMessagesData returns an ErrRetrievingContentMessagesData wrapping err.
func NewErrConvertingContentMessagesData(err error) *ErrRetrievingContentMessagesData {
	return &ErrRetrievingContentMessagesData{
		BaseError: &BaseError{
			Err:     err,
			Message: "there was an error converting content messages data",
		},
	}
}

// ErrRetrievingContentMessages reports a failure reading messages from a session.
type ErrRetrievingContentMessages struct {
	*BaseError
}

// NewErrRetrievingContentMessages returns an ErrRetrievingContentMessages wrapping err.
func NewErrRetrievingContentMessages(err error) *ErrRetrievingContentMessages {
	return &ErrRetrievingContentMessages{
		BaseError: &BaseError{
			Err:     err,
			Message: "there was an error retrieving content messages from session storage",
		},
	}
}

// ErrSavingContentMessages reports a failure saving messages to a session.
type ErrSavingContentMessages struct {
	*BaseError
}

// NewErrSavingContentMessages returns an ErrSavingContentMessages wrapping err.
func NewErrSavingContentMessages(err error) *ErrSavingContentMessages {
	return &ErrSavingContentMessages{
		BaseError: &BaseError{
			Err:     err,
			Message: "there was an error saving content messages to session storage",
		},
	}
}

// ErrDeletingContentMessages reports a failure removing stored messages.
type ErrDeletingContentMessages struct {
	*BaseError
}

// NewErrDeletingContentMessages returns an ErrDeletingContentMessages wrapping err.
func NewErrDeletingContentMessages(err error) *ErrDeletingContentMessages {
	return &ErrDeletingContentMessages{
		BaseError: &BaseError{
			Err:     err,
			Message: "there was an error deleting saved content messages from session storage",
		},
	}
}

// ErrMessageCategoryNotSpecified indicates that a flash-message category is missing.
type ErrMessageCategoryNotSpecified struct {
	*BaseError
}

// NewErrMessageCategoryNotSpecified returns an ErrMessageCategoryNotSpecified wrapping err.
func NewErrMessageCategoryNotSpecified(err error) *ErrMessageCategoryNotSpecified {
	return &ErrMessageCategoryNotSpecified{
		BaseError: &BaseError{
			Err:     err,
			Message: "a message category is required",
		},
	}
}
