// errors.go defines custom errors for the messages package
package messages

import (
	hs_errors "github.com/trentnix/hyperserver/pkg/errors"
)

type BaseError = hs_errors.BaseError

type ErrRetrievingContentMessagesData struct {
	*BaseError
}

func NewErrConvertingContentMessagesData(err error) *ErrRetrievingContentMessagesData {
	return &ErrRetrievingContentMessagesData{
		BaseError: &BaseError{
			Err:     err,
			Message: "there was an error converting content messages data",
		},
	}
}

type ErrRetrievingContentMessages struct {
	*BaseError
}

func NewErrRetrievingContentMessages(err error) *ErrRetrievingContentMessages {
	return &ErrRetrievingContentMessages{
		BaseError: &BaseError{
			Err:     err,
			Message: "there was an error retrieving content messages from session storage",
		},
	}
}

type ErrSavingContentMessages struct {
	*BaseError
}

func NewErrSavingContentMessages(err error) *ErrSavingContentMessages {
	return &ErrSavingContentMessages{
		BaseError: &BaseError{
			Err:     err,
			Message: "there was an error saving content messages to session storage",
		},
	}
}

type ErrDeletingContentMessages struct {
	*BaseError
}

func NewErrDeletingContentMessages(err error) *ErrDeletingContentMessages {
	return &ErrDeletingContentMessages{
		BaseError: &BaseError{
			Err:     err,
			Message: "there was an error deleting saved content messages from session storage",
		},
	}
}

type ErrMessageCategoryNotSpecified struct {
	*BaseError
}

func NewErrMessageCategoryNotSpecified(err error) *ErrMessageCategoryNotSpecified {
	return &ErrMessageCategoryNotSpecified{
		BaseError: &BaseError{
			Err:     err,
			Message: "a message category is required",
		},
	}
}
