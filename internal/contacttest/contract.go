// Package contacttest checks the development site's contact-storage contract.
// It is test support, not an application storage implementation.
package contacttest

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	site "github.com/trentnix/hyperserver/modules/site"
	"github.com/trentnix/hyperserver/modules/site/models"
)

// Fixture supplies a repository and test-only access to its storage.
// ReadAll must read committed records independently of Create's output.
// SetWriteFailure enables or disables a storage failure without removing records
// or preventing reads. Neither hook adds methods to the application contract.
type Fixture struct {
	Repository      site.ContactRepository
	ReadAll         func(*testing.T) []models.ContactSubmission
	SetWriteFailure func(*testing.T, bool)
}

// Run checks contact creation. Each factory call must provide isolated, ready
// storage and register cleanup with t. Backend-specific setup, I/O failure,
// cancellation during I/O, clock accuracy, timestamp precision, and durability
// tests remain with the provider. Returned timestamps must match stored values.
func Run(t *testing.T, open func(*testing.T) Fixture) {
	t.Helper()
	for _, supplied := range []bool{false, true} {
		t.Run(fmt.Sprintf("generated fields/supplied values=%t", supplied), func(t *testing.T) {
			fixture := open(t)
			assertRecords(t, fixture)
			submission := exampleSubmission()
			if !supplied {
				submission.Id, submission.CreatedAt = "", time.Time{}
			}
			before := submission
			must(t, fixture.Repository.Create(t.Context(), &submission))
			assertCreated(t, submission, before)
			assertRecords(t, fixture, submission)

			stored := submission
			submission.Name, submission.Email, submission.Message = "changed", "changed@example.invalid", "not saved"
			submission.Id, submission.CreatedAt = "changed", time.Time{}
			assertRecords(t, fixture, stored)
		})
	}

	for _, reuse := range []bool{false, true} {
		t.Run(fmt.Sprintf("repeated content/reuse submission=%t", reuse), func(t *testing.T) {
			fixture := open(t)
			submission := exampleSubmission()
			must(t, fixture.Repository.Create(t.Context(), &submission))
			first := submission
			second := &submission
			if !reuse {
				fresh := exampleSubmission()
				second = &fresh
			}
			before := *second
			must(t, fixture.Repository.Create(t.Context(), second))
			assertCreated(t, *second, before)
			if first.Id == second.Id {
				t.Fatal("repeated content reused a stored ID")
			}
			assertRecords(t, fixture, first, *second)
		})
	}

	for _, failure := range []string{"nil submission", "canceled request", "storage failure"} {
		t.Run("failed create/"+failure, func(t *testing.T) {
			fixture := open(t)
			existing := exampleSubmission()
			must(t, fixture.Repository.Create(t.Context(), &existing))
			submission := exampleSubmission()
			submission.Name, submission.Message = "Another sender", "A different message"
			before := submission
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			input := &submission
			switch failure {
			case "nil submission":
				input = nil
			case "canceled request":
				cancel()
			case "storage failure":
				fixture.SetWriteFailure(t, true)
			}

			err := fixture.Repository.Create(ctx, input)
			if err == nil {
				t.Fatal("failed create reported success")
			}
			if failure == "canceled request" && !errors.Is(err, context.Canceled) {
				t.Fatalf("canceled create error = %v", err)
			}
			if submission != before {
				t.Fatal("failed create changed its input")
			}
			assertRecords(t, fixture, existing)

			if failure == "storage failure" {
				fixture.SetWriteFailure(t, false)
			}
			must(t, fixture.Repository.Create(t.Context(), &submission))
			assertCreated(t, submission, before)
			assertRecords(t, fixture, existing, submission)
		})
	}

	t.Run("concurrent creates", func(t *testing.T) {
		fixture := open(t)
		existing := exampleSubmission()
		must(t, fixture.Repository.Create(t.Context(), &existing))
		const count = 12
		var inputs, before [count]models.ContactSubmission
		var errs [count]error
		var work sync.WaitGroup
		start := make(chan struct{})
		for i := range inputs {
			inputs[i] = exampleSubmission()
			inputs[i].Name = fmt.Sprintf("Sender %d", i)
			inputs[i].Message = fmt.Sprintf("Message %d", i)
			before[i] = inputs[i]
			work.Go(func() {
				<-start
				errs[i] = fixture.Repository.Create(t.Context(), &inputs[i])
			})
		}
		close(start)
		// Collect all results before checking them, so cleanup cannot race a writer.
		work.Wait()
		for i, err := range errs {
			must(t, err)
			assertCreated(t, inputs[i], before[i])
		}
		assertRecords(t, fixture, append(inputs[:], existing)...)
	})
}

func exampleSubmission() models.ContactSubmission {
	return models.ContactSubmission{
		Id: "caller-supplied ID", CreatedAt: time.Date(2000, 1, 2, 3, 4, 5, 0, time.UTC),
		Name: "O'Neil 世界", Email: "Mixed.Case@example.invalid", Message: "Hello <world>\n'); DROP TABLE user; --",
	}
}

func assertCreated(t *testing.T, got, before models.ContactSubmission) {
	t.Helper()
	if got.Id == "" || got.Id == before.Id {
		t.Fatal("create did not generate a new ID")
	}
	if got.CreatedAt.IsZero() {
		t.Fatal("create did not supply a creation time")
	}
	if got.Name != before.Name || got.Email != before.Email || got.Message != before.Message {
		t.Fatalf("create changed submitted content: %+v, want %+v", got, before)
	}
}

func assertRecords(t *testing.T, fixture Fixture, want ...models.ContactSubmission) {
	t.Helper()
	got := fixture.ReadAll(t)
	if len(got) != len(want) {
		t.Fatalf("stored records = %d, want %d", len(got), len(want))
	}
	byID := make(map[string]models.ContactSubmission, len(want))
	for _, record := range want {
		if _, exists := byID[record.Id]; exists {
			t.Fatalf("creates reused ID %q", record.Id)
		}
		record.CreatedAt = record.CreatedAt.Round(0).UTC()
		byID[record.Id] = record
	}
	for _, record := range got {
		record.CreatedAt = record.CreatedAt.Round(0).UTC()
		if expected, ok := byID[record.Id]; !ok || record != expected {
			t.Fatalf("unexpected stored record: %+v, want %+v", record, expected)
		}
		delete(byID, record.Id)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
