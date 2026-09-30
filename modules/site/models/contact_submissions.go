// Package models stores contact submissions for the development site.
package models

import (
	"fmt"
	"net/mail"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	site_db "github.com/trentnix/hyperserver/modules/site/database"
	"github.com/trentnix/hyperserver/pkg/database"

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

const (
	contactSubmissionsTable = "hyperserver_contact_submission"
)

// Create adds the specified ContactSubmission to the database
func (contactSubmission *ContactSubmission) Create(db *sqlx.DB) error {
	if err := site_db.PrepareDatabase(db.DB, contactSubmissionsTable); err != nil {
		return err
	}

	if !IsValidEmail(contactSubmission.Email) {
		// the email provided is invalid
		return NewErrInvalidEmail(fmt.Errorf("failed to create a new contact_submission record"))
	}

	contactSubmission.Id = uuid.New().String()
	contactSubmission.CreatedAt = time.Now()

	query := fmt.Sprintf(`
		INSERT INTO %s (id, name, email, message, created_at)
		VALUES (:id, :name, :email, :message, :created_at)
		`, contactSubmissionsTable)

	_, err := db.NamedExec(query, contactSubmission)
	if err != nil {
		return database.NewErrDatabase(err)
	}

	return nil
}

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
