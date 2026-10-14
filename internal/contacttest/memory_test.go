package contacttest

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/trentnix/hyperserver/modules/site/models"
)

// memoryRepository is a test adapter, not a supported application provider.
// It copies submissions and serializes writes without using a database.
type memoryRepository struct {
	mu         sync.Mutex
	records    []models.ContactSubmission
	failWrites bool
}

func (m *memoryRepository) Create(ctx context.Context, submission *models.ContactSubmission) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if submission == nil {
		return errors.New("a contact submission must be specified")
	}
	if m.failWrites {
		return errors.New("injected contact storage failure")
	}

	created := *submission
	created.Id, created.CreatedAt = uuid.NewString(), time.Now()
	m.records = append(m.records, created)
	*submission = created
	return nil
}

func TestMemoryContactContract(t *testing.T) {
	Run(t, func(*testing.T) Fixture {
		repo := &memoryRepository{}
		return Fixture{
			Repository: repo,
			ReadAll: func(*testing.T) []models.ContactSubmission {
				repo.mu.Lock()
				defer repo.mu.Unlock()
				return slices.Clone(repo.records)
			},
			SetWriteFailure: func(_ *testing.T, fail bool) {
				repo.mu.Lock()
				defer repo.mu.Unlock()
				repo.failWrites = fail
			},
		}
	})
}
