// Package models stores contact submissions for the development site.
package models

import (
	"context"
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

// ContactRepository stores the site's contact submissions using a borrowed pool.
// It owns no connections or background work and needs no Close method.
type ContactRepository struct {
	db *sqlx.DB
}

// NewContactRepository prepares the site's tables before returning a repository.
// The caller must keep db open until all repository operations have finished.
func NewContactRepository(ctx context.Context, db *sqlx.DB) (*ContactRepository, error) {
	if db == nil {
		return nil, database.NewErrDatabaseUnavailable(nil)
	}
	if err := site_db.PrepareDatabase(ctx, db.DB); err != nil {
		return nil, err
	}
	return &ContactRepository{db: db}, nil
}

// Create stores a contact submission. Failed writes leave the submission unchanged.
func (repository *ContactRepository) Create(ctx context.Context, contactSubmission *ContactSubmission) error {
	if contactSubmission == nil {
		return fmt.Errorf("a contact submission must be specified")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	if !IsValidEmail(contactSubmission.Email) {
		// the email provided is invalid
		return NewErrInvalidEmail(fmt.Errorf("failed to create a new contact_submission record"))
	}

	created := *contactSubmission
	created.Id = uuid.New().String()
	created.CreatedAt = time.Now()

	query := fmt.Sprintf(`
		INSERT INTO %s (id, name, email, message, created_at)
		VALUES (:id, :name, :email, :message, :created_at)
		`, contactSubmissionsTable)

	_, err := repository.db.NamedExecContext(ctx, query, &created)
	if err != nil {
		return database.NewErrDatabase(err)
	}
	*contactSubmission = created

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
