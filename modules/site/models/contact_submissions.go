// contact_submissions.go defines the ContactSubmission struct and the code to
// serialize it to and from the specified database connection
package models

import (
	"fmt"
	"net/mail"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/trentnix/hyperserver/modules/site/database"
)

type (
	ContactSubmission struct {
		Name      string    `db:"name"`
		Email     string    `db:"email"`
		Message   string    `db:"message"`
		CreatedAt time.Time `db:"created_at"`
	}
)

const (
	contactSubmissionsTable = "hyperserver_contact_submission"
)

// Create adds the specified ContactSubmission to the database
func (contactSubmission *ContactSubmission) Create(db *sqlx.DB) error {
	if err := database.PrepareDatabase(db.DB, contactSubmissionsTable); err != nil {
		return err
	}

	contactSubmission.CreatedAt = time.Now()

	query := fmt.Sprintf(`
		INSERT INTO %s (name, email, message, created_at)
		VALUES (:name, :email, :message, :created_at)
		`, contactSubmissionsTable)

	_, err := db.NamedExec(query, contactSubmission)
	return err
}

// IsValidEmail confirms that the email provided is a validly constructed email address.
func IsValidEmail(email string) bool {
	_, err := mail.ParseAddress(email)
	return err == nil
}
