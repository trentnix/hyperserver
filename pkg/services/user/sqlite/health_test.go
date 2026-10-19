package sqlite

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/trentnix/hyperserver/pkg/database"
	accounts "github.com/trentnix/hyperserver/pkg/services/user"
)

func TestAccountHealth(t *testing.T) {
	db := userTestDB(t)
	repository := NewAccountRepository(db)
	if err := repository.CheckHealth(t.Context()); err != nil {
		t.Fatal("empty account table:", err)
	}
	account := &accounts.User{Email: "reader@example.com", Password: "hash"}
	if err := repository.Create(t.Context(), account); err != nil {
		t.Fatal(err)
	}
	before, err := repository.GetByID(t.Context(), account.ID)
	if err != nil {
		t.Fatal(err)
	}

	var calls sync.WaitGroup
	for range 16 {
		calls.Go(func() {
			if err := repository.CheckHealth(t.Context()); err != nil {
				t.Error(err)
			}
		})
	}
	calls.Wait()
	after, err := repository.GetByID(t.Context(), account.ID)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("probe changed account: %+v, %v", after, err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := repository.CheckHealth(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled probe: %v", err)
	}
	if _, err := db.ExecContext(t.Context(), "DROP TABLE user"); err != nil {
		t.Fatal(err)
	}
	if err := repository.CheckHealth(t.Context()); err == nil {
		t.Fatal("missing account table reported healthy")
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := repository.CheckHealth(t.Context()); err == nil {
		t.Fatal("closed storage reported healthy")
	}
	if err := NewAccountRepository(nil).CheckHealth(t.Context()); !errors.Is(err, database.ErrDatabaseUnavailable) {
		t.Fatalf("unconfigured repository: %v", err)
	}
}

func TestAccountHealthHonorsDeadlineWhileWaiting(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		db := userTestDB(t)
		repository := NewAccountRepository(db)
		// Holding the only connection forces the probe to wait for its deadline.
		connection, err := db.Conn(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		defer connection.Close()

		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		if err := repository.CheckHealth(ctx); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("waiting probe: %v", err)
		}
	})
}
