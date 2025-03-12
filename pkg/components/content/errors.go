// errors.go defines custom errors in the content package
package content

import (
	hs_errors "github.com/trentnix/hyperserver/pkg/errors"
)

type BaseError = hs_errors.BaseError

// ErrNoTemplates indicates no templates are selected for rendering
type ErrNoTemplates struct {
	*BaseError
}

// NewErrNoTemplates creates an instance of ErrNoTemplates
func NewErrNoTemplates(err error) *ErrNoTemplates {
	return &ErrNoTemplates{
		BaseError: &BaseError{
			Err:     err,
			Message: "no templates specified",
		},
	}
}

// ErrLoadingTemplates indicates an error parsing the specified templates
type ErrParsingTemplates struct {
	*BaseError
}

// NewErrNoTemplates creates an instance of ErrNoTemplates
func NewErrParsingTemplates(err error) *ErrParsingTemplates {
	return &ErrParsingTemplates{
		BaseError: &BaseError{
			Err:     err,
			Message: "unable to parse the specified templates",
		},
	}
}

// ErrRenderingTemplates indicates an error rendering the specified templates
type ErrRenderingTemplates struct {
	*BaseError
}

// NewErrRenderingTemplates creates an instance of ErrRenderingTemplates
func NewErrRenderingTemplates(err error) *ErrRenderingTemplates {
	return &ErrRenderingTemplates{
		BaseError: &BaseError{
			Err:     err,
			Message: "unable to render the specified templates",
		},
	}
}

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
