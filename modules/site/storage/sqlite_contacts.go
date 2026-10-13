// Package storage provides contact repositories for the development site.
package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	site_db "github.com/trentnix/hyperserver/modules/site/database"
	"github.com/trentnix/hyperserver/modules/site/models"
	"github.com/trentnix/hyperserver/pkg/database"
)

// SQLiteContactRepository stores contact submissions using a borrowed pool.
// It owns no connections or background work and needs no Close method.
type SQLiteContactRepository struct {
	db *sql.DB
}

// NewSQLiteContactRepository prepares the site's tables before returning a repository.
// The caller must keep db open until all repository operations have finished.
func NewSQLiteContactRepository(ctx context.Context, db *sql.DB) (*SQLiteContactRepository, error) {
	if db == nil {
		return nil, database.NewErrDatabaseUnavailable(nil)
	}
	if err := site_db.PrepareDatabase(ctx, db); err != nil {
		return nil, err
	}
	return &SQLiteContactRepository{db: db}, nil
}

// Create stores a contact submission. Failed writes leave the submission unchanged.
func (repository *SQLiteContactRepository) Create(ctx context.Context, submission *models.ContactSubmission) error {
	if submission == nil {
		return fmt.Errorf("a contact submission must be specified")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !models.IsValidEmail(submission.Email) {
		return models.NewErrInvalidEmail(fmt.Errorf("failed to create a new contact_submission record"))
	}

	created := *submission
	created.Id = uuid.New().String()
	created.CreatedAt = time.Now()

	const query = `INSERT INTO hyperserver_contact_submission (id, name, email, message, created_at)
		VALUES (?, ?, ?, ?, ?)`
	if _, err := repository.db.ExecContext(ctx, query, created.Id, created.Name, created.Email, created.Message, created.CreatedAt); err != nil {
		return database.NewErrDatabase(err)
	}
	*submission = created
	return nil
}
