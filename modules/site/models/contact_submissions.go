// contact_submissions.go defines the ContactSubmission struct and the code to
// serialize it to and from the specified database connection
package models

import (
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
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
	if err := validateDatabase(db.DB, contactSubmissionsTable); err != nil {
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
