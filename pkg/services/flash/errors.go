package flash

import "errors"

var (
	// ErrRetrievingContentMessages reports a failure reading messages from a session.
	ErrRetrievingContentMessages = errors.New("there was an error retrieving content messages from session storage")
	// ErrSavingContentMessages reports a failure saving messages to a session.
	ErrSavingContentMessages = errors.New("there was an error saving content messages to session storage")
	// ErrDeletingContentMessages reports a failure removing stored messages.
	ErrDeletingContentMessages = errors.New("there was an error deleting saved content messages from session storage")
	// ErrMessageCategoryNotSpecified indicates that a flash-message category is missing.
	ErrMessageCategoryNotSpecified = errors.New("a message category is required")
)
