// Package models defines contact submissions for the development site.
package models

import (
	"errors"
	"net/mail"
	"time"
)

type (
	// ContactSubmission is a contact message persisted by the development site.
	ContactSubmission struct {
		Id        string
		Name      string
		Email     string
		Message   string
		CreatedAt time.Time
	}
)

// ErrInvalidEmail reports an invalid email address in a contact submission.
var ErrInvalidEmail = errors.New("invalid email address")

// IsValidEmail confirms that the email provided is a validly constructed email address.
func IsValidEmail(email string) bool {
	_, err := mail.ParseAddress(email)
	return err == nil
}
