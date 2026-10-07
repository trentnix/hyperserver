package user

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/database"
	"github.com/trentnix/hyperserver/pkg/services/session"
)

func TestSQLiteAccountRepository(t *testing.T) {
	for _, by := range []string{"ID", "email"} {
		t.Run(by, func(t *testing.T) {
			db := userTestDB(t)
			reader := NewSQLiteAccountRepository(db)
			account := &User{Email: "reader@example.invalid", Password: "hash", Verified: true, VerificationRequired: true, RegistrationAuthType: "email"}
			if err := reader.Create(context.Background(), account); err != nil {
				t.Fatal(err)
			}
			if account.ID == "" || account.SessionVersion != 1 || account.CreatedAt.IsZero() || account.UpdatedAt.IsZero() {
				t.Fatal("creation did not supply account identity, timestamps, and session version")
			}
			lookup, key := reader.GetByID, account.ID
			if by == "email" {
				lookup, key = reader.GetByEmail, account.Email
			}
			stored, err := lookup(context.Background(), key)
			if err != nil || stored.ID != account.ID || stored.Email != account.Email || stored.Password != account.Password || !stored.Verified || !stored.VerificationRequired || stored.RegistrationAuthType != "email" || stored.SessionVersion != 1 || stored.CreatedAt.IsZero() || stored.UpdatedAt.IsZero() {
				t.Fatalf("account fields were lost: %+v, %v", stored, err)
			}
			again, err := lookup(context.Background(), key)
			if err != nil || !reflect.DeepEqual(stored, again) || stored == again {
				t.Fatal("reads must return independent account values:", err)
			}
			var notFound *ErrUserNotFound
			if got, err := lookup(context.Background(), "absent"); got != nil || !errors.As(err, &notFound) {
				t.Fatalf("missing account = %v, %v", got, err)
			}
			if _, err := lookup(context.Background(), ""); err == nil {
				t.Fatal("empty lookup accepted")
			}

			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if _, err := lookup(ctx, key); !errors.Is(err, context.Canceled) {
				t.Fatal("lookup lost cancellation:", err)
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			if got, err := lookup(context.Background(), key); got != nil || err == nil || errors.As(err, &notFound) {
				t.Fatalf("storage failure treated as missing account: %v, %v", got, err)
			}
		})
	}
}

func TestSQLiteAccountRepositoryCreateFailure(t *testing.T) {
	for _, scenario := range []string{"duplicate", "nil pool", "closed pool", "canceled", "write failure"} {
		t.Run(scenario, func(t *testing.T) {
			db := userTestDB(t)
			repository := NewSQLiteAccountRepository(db)
			account := &User{Email: "new@example.invalid", Password: "new hash", VerificationRequired: true}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var existing *User
			switch scenario {
			case "duplicate":
				u := &User{Email: account.Email, Password: "original hash", Verified: true}
				if err := repository.Create(ctx, u); err != nil {
					t.Fatal(err)
				}
				var err error
				existing, err = repository.GetByID(ctx, u.ID)
				if err != nil {
					t.Fatal(err)
				}
			case "nil pool":
				repository = NewSQLiteAccountRepository(nil)
			case "closed pool":
				if err := db.Close(); err != nil {
					t.Fatal(err)
				}
			case "canceled":
				cancel()
			case "write failure":
				if _, err := db.Exec(`CREATE TRIGGER reject_create BEFORE INSERT ON user BEGIN SELECT RAISE(ABORT, 'injected failure'); END`); err != nil {
					t.Fatal(err)
				}
			}

			before := *account
			err := repository.Create(ctx, account)
			if err == nil || *account != before {
				t.Fatalf("failed insertion succeeded or changed its input: %v", err)
			}
			if scenario == "canceled" && !errors.Is(err, context.Canceled) {
				t.Fatal("lost cancellation:", err)
			}
			if existing != nil {
				var duplicate *database.ErrRecordAlreadyExists
				if !errors.As(err, &duplicate) {
					t.Fatal("duplicate email error lost:", err)
				}
				stored, err := repository.GetByID(context.Background(), existing.ID)
				if err != nil || !reflect.DeepEqual(stored, existing) {
					t.Fatal("failed insertion changed an existing account:", err)
				}
			} else if scenario != "closed pool" {
				var count int
				if err := db.Get(&count, "SELECT count(*) FROM user"); err != nil || count != 0 {
					t.Fatalf("failed insertion left an account: count=%d, error=%v", count, err)
				}
			}
		})
	}
}

func TestSQLiteAccountRepositoryCreateCancellationWhileWaiting(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		db := userTestDB(t)
		// Hold the only connection so Create must wait for the request context.
		connection, err := db.Conn(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		defer connection.Close()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		account := &User{Email: "waiting@example.invalid"}
		before := *account
		result := make(chan error, 1)
		go func() { result <- NewSQLiteAccountRepository(db).Create(ctx, account) }()
		synctest.Wait()
		select {
		case err := <-result:
			t.Fatalf("Create did not wait for the connection: %v", err)
		default:
		}
		cancel()
		if err := <-result; !errors.Is(err, context.Canceled) || *account != before {
			t.Fatalf("canceled insertion changed its input or lost cancellation: %v", err)
		}
		if err := connection.Close(); err != nil {
			t.Fatal(err)
		}
		var count int
		if err := db.Get(&count, "SELECT count(*) FROM user"); err != nil || count != 0 {
			t.Fatalf("canceled insertion left an account: count=%d, error=%v", count, err)
		}
	})
}

func TestSQLiteAccountRepositoryRequiresPreparedStorage(t *testing.T) {
	for _, reader := range []*SQLiteAccountRepository{NewSQLiteAccountRepository(nil), NewSQLiteAccountRepository(unpreparedUserTestDB(t))} {
		for _, lookup := range []func(context.Context, string) (*User, error){reader.GetByID, reader.GetByEmail} {
			var notFound *ErrUserNotFound
			if got, err := lookup(context.Background(), "account"); got != nil || err == nil || errors.As(err, &notFound) {
				t.Fatalf("unavailable storage treated as missing account: %v, %v", got, err)
			}
		}
	}
}

func TestSQLiteAccountRepositoryConcurrentPasswordChanges(t *testing.T) {
	db := userTestDB(t)
	repository := NewSQLiteAccountRepository(db)
	u := &User{Email: "change@example.invalid", Password: "old hash", RegistrationAuthType: "email"}
	if err := repository.Create(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	u, err := repository.GetByID(context.Background(), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	before := *u
	accounts := [2]User{before, before}
	hashes := [2]string{"first hash", "second hash"}
	var results [2]error
	var workers sync.WaitGroup
	start := make(chan struct{})
	for i := range accounts {
		workers.Go(func() {
			<-start
			results[i] = repository.ChangePassword(context.Background(), &accounts[i], hashes[i])
		})
	}
	close(start)
	workers.Wait()

	stored, err := repository.GetByID(context.Background(), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	successes := 0
	for i, err := range results {
		if err != nil {
			if accounts[i] != before {
				t.Error("rejected change mutated its account")
			}
			continue
		}
		successes++
		want := before
		want.Password, want.UpdatedAt = hashes[i], stored.UpdatedAt
		want.SessionVersion++
		if *stored != want || accounts[i].Password != want.Password || accounts[i].SessionVersion != want.SessionVersion || !accounts[i].UpdatedAt.Equal(want.UpdatedAt) {
			t.Error("successful change did not commit exactly one password and session-version update")
		}
	}
	if successes != 1 {
		t.Fatalf("successful changes = %d, want 1: %v", successes, results)
	}
}

func TestSQLiteAccountRepositoryChangePasswordCancellationWhileWaiting(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		db := userTestDB(t)
		repository := NewSQLiteAccountRepository(db)
		u := &User{Email: "change@example.invalid", Password: "old hash", RegistrationAuthType: "email"}
		if err := repository.Create(context.Background(), u); err != nil {
			t.Fatal(err)
		}
		u, err := repository.GetByID(context.Background(), u.ID)
		if err != nil {
			t.Fatal(err)
		}
		before := *u
		connection, err := db.Conn(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		defer connection.Close()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		result := make(chan error, 1)
		go func() { result <- repository.ChangePassword(ctx, u, "new hash") }()
		synctest.Wait()
		select {
		case err := <-result:
			t.Fatalf("change returned before a connection was available: %v", err)
		default:
		}

		cancel()
		if err := <-result; !errors.Is(err, context.Canceled) || *u != before {
			t.Fatalf("canceled change did not preserve the account: %v", err)
		}
		if err := connection.Close(); err != nil {
			t.Fatal(err)
		}
		stored, err := repository.GetByID(context.Background(), u.ID)
		if err != nil || *stored != before {
			t.Fatalf("canceled change altered storage: %v", err)
		}
	})
}

func TestSQLiteAccountRepositoryUpdateCancellationWhileWaiting(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		db, u, token, key := verificationFixture(t)
		repository := NewSQLiteAccountRepository(db)
		storedBefore, err := repository.GetByID(context.Background(), u.ID)
		if err != nil {
			t.Fatal(err)
		}
		u.Email = "changed@example.invalid"
		before := *u
		// Hold the only connection so Update must wait before starting its transaction.
		connection, err := db.Conn(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		defer connection.Close()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		result := make(chan error, 1)
		go func() { result <- repository.Update(ctx, u) }()
		synctest.Wait()
		select {
		case err := <-result:
			t.Fatalf("update returned before a connection was available: %v", err)
		default:
		}

		cancel()
		if err := <-result; !errors.Is(err, context.Canceled) || *u != before {
			t.Fatalf("canceled update changed its input or lost cancellation: %v", err)
		}
		if err := connection.Close(); err != nil {
			t.Fatal(err)
		}
		stored, err := repository.GetByID(context.Background(), u.ID)
		if err != nil || *stored != *storedBefore {
			t.Fatalf("canceled update changed the stored account: %v", err)
		}
		if _, err := ValidateVerificationToken(context.Background(), NewSQLiteAccountRepository(db), token.Token, key); err != nil {
			t.Fatalf("canceled update revoked the verification token: %v", err)
		}
	})
}

type accountReaderStub struct {
	AccountRepository
	lookup func(context.Context, string) (*User, error)
}

func (s accountReaderStub) GetByID(ctx context.Context, id string) (*User, error) {
	return s.lookup(ctx, id)
}

func TestGetAuthenticatedUserUsesAccountRepository(t *testing.T) {
	cfg := &config.Config{}
	cfg.HTTP.Session.JwtKey = "account-reader-test-key"
	cfg.HTTP.Session.TokenAge, cfg.HTTP.Session.CookieAge = time.Hour, time.Hour
	cfg.HTTP.Session.Stores = map[string]map[string]string{"cookieStore": {"enabled": "true"}}
	cfg.HTTP.Session.Types = map[string]string{"default": "cookieStore"}
	manager, err := session.NewSessionManager(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	setup := session.AddSessionManagerToRequestContext(httptest.NewRequest(http.MethodGet, "/", nil), manager)
	value, err := session.New(setup, userSessionKey)
	if err != nil {
		t.Fatal(err)
	}
	value.Data[userSessionKey], value.Data[userSessionVersionKey] = "account", int64(1)
	w := httptest.NewRecorder()
	if err := value.Save(w, setup); err != nil {
		t.Fatal(err)
	}

	for _, scenario := range []string{"success", "missing", "storage failure", "canceled", "revoked"} {
		t.Run(scenario, func(t *testing.T) {
			r := session.AddSessionManagerToRequestContext(httptest.NewRequest(http.MethodGet, "/", nil), manager)
			ctx, cancel := context.WithCancel(r.Context())
			defer cancel()
			r = r.WithContext(ctx)
			for _, cookie := range w.Result().Cookies() {
				r.AddCookie(cookie)
			}
			var wantErr error
			switch scenario {
			case "missing":
				wantErr = NewErrUserNotFound(nil)
			case "storage failure":
				wantErr = errors.New("storage unavailable")
			case "canceled":
				cancel()
				wantErr = context.Canceled
			}
			calls := 0
			reader := accountReaderStub{lookup: func(got context.Context, id string) (*User, error) {
				calls++
				if got != r.Context() || got.Err() != ctx.Err() || id != "account" {
					t.Fatal("identity lookup lost the request context or ID")
				}
				if wantErr != nil {
					return nil, wantErr
				}
				version := int64(1)
				if scenario == "revoked" {
					version++
				}
				return &User{ID: id, SessionVersion: version}, nil
			}}
			got, err := GetAuthenticatedUser(r, reader)
			if calls != 1 || !errors.Is(err, wantErr) || (got != nil) != (scenario == "success") {
				t.Fatalf("identity result = %v, %v, calls=%d", got, err, calls)
			}
		})
	}
}
