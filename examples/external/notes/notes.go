// Package notes implements the example's append-only notes feature.
package notes

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// MaxTextLength limits notes to this many Unicode code points.
const MaxTextLength = 1000

// MaxListSize bounds the number of notes returned by a listing.
const MaxListSize = 100

// ErrInvalidText reports text that violates the note's content limits.
var ErrInvalidText = errors.New("enter a note of 1 to 1000 characters")

// ErrExists reports an attempted insert with an existing note ID.
var ErrExists = errors.New("note already exists")

// Note is immutable after creation. Text is plain text, not trusted HTML.
type Note struct {
	ID        string    `json:"id"`
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"createdAt"`
}

// Repository stores complete notes without replacing an existing ID. Concurrent
// calls must be safe. List returns up to limit notes, newest first, breaking ties
// by ascending ID. Limits must be between 1 and MaxListSize. Errors must not be
// reported as empty results. Cancellation after a write commits cannot undo it.
type Repository interface {
	Create(context.Context, Note) error
	List(context.Context, int) ([]Note, error)
}

// ValidateText checks the same text rule used by forms and storage providers.
func ValidateText(text string) error {
	if !utf8.ValidString(text) || strings.TrimSpace(text) == "" || utf8.RuneCountInString(text) > MaxTextLength {
		return ErrInvalidText
	}
	return nil
}

// Validate checks a note's identity, timestamp, and text.
func Validate(note Note) error {
	id, err := uuid.Parse(note.ID)
	if err != nil || id.String() != note.ID || note.CreatedAt.IsZero() || note.CreatedAt.UTC().Year() < 1 || note.CreatedAt.UTC().Year() > 9999 {
		return fmt.Errorf("invalid note identity or timestamp")
	}
	return ValidateText(note.Text)
}

// Create validates input and assigns identity before calling the selected repository.
func Create(ctx context.Context, repository Repository, text string) (Note, error) {
	if err := ctx.Err(); err != nil {
		return Note{}, err
	}
	text = strings.TrimSpace(text)
	if err := ValidateText(text); err != nil {
		return Note{}, err
	}
	note := Note{ID: uuid.NewString(), Text: text, CreatedAt: time.Now().UTC()}
	if err := repository.Create(ctx, note); err != nil {
		return Note{}, err
	}
	return note, nil
}
