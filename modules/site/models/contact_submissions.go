// Package models defines contact submissions for the development site.
package models

import (
	"net/mail"
	"time"

	hs_errors "github.com/trentnix/hyperserver/pkg/errors"
)

type (
	// ContactSubmission is a contact message persisted by the development site.
	ContactSubmission struct {
		Id        string    `db:"id"`
		Name      string    `db:"name"`
		Email     string    `db:"email"`
		Message   string    `db:"message"`
		CreatedAt time.Time `db:"created_at"`
	}

	// BaseError aliases the shared HyperServer error wrapper.
	BaseError = hs_errors.BaseError

	// ErrInvalidEmail indicates an email address field is invalid
	ErrInvalidEmail struct {
		*BaseError
	}
)

// IsValidEmail confirms that the email provided is a validly constructed email address.
func IsValidEmail(email string) bool {
	_, err := mail.ParseAddress(email)
	return err == nil
}

// NewErrInvalidEmail returns an ErrInvalidEmail wrapping err.
func NewErrInvalidEmail(err error) *ErrInvalidEmail {
	return &ErrInvalidEmail{
		BaseError: &BaseError{
			Err:     err,
			Message: "invalid email address",
		},
	}
}
