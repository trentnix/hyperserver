package sqlite

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/mattn/go-sqlite3"
	accounts "github.com/trentnix/hyperserver/pkg/services/user"
)

func TestAccountWriteError(t *testing.T) {
	unique := sqlite3.Error{Code: sqlite3.ErrConstraint, ExtendedCode: sqlite3.ErrConstraintUnique}
	for _, tc := range []struct {
		name      string
		err       error
		duplicate bool
	}{
		{"nil", nil, false},
		{"duplicate email", unique, true},
		{"wrapped duplicate email", fmt.Errorf("insert account: %w", unique), true},
		{"primary key", sqlite3.Error{Code: sqlite3.ErrConstraint, ExtendedCode: sqlite3.ErrConstraintPrimaryKey}, false},
		{"not null", sqlite3.Error{Code: sqlite3.ErrConstraint, ExtendedCode: sqlite3.ErrConstraintNotNull}, false},
		{"check", sqlite3.Error{Code: sqlite3.ErrConstraint, ExtendedCode: sqlite3.ErrConstraintCheck}, false},
		{"trigger", sqlite3.Error{Code: sqlite3.ErrConstraint, ExtendedCode: sqlite3.ErrConstraintTrigger}, false},
		{"storage failure", errors.New("storage unavailable"), false},
		{"canceled", context.Canceled, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := accountWriteError(tc.err)
			if errors.Is(err, accounts.ErrEmailAlreadyExists) != tc.duplicate {
				t.Fatalf("error = %v, want duplicate = %t", err, tc.duplicate)
			}
			if !errors.Is(err, tc.err) {
				t.Fatalf("error lost its cause: %v", err)
			}
			if !tc.duplicate && err != tc.err {
				t.Fatalf("unrelated error was changed: %v", err)
			}
		})
	}
}

func TestSQLiteAccountRepositoryInitialize(t *testing.T) {
	db := unpreparedUserTestDB(t)
	repository := NewAccountRepository(db)
	assertNoAccountTables(t, db)
	ctx := context.Background()
	if err := repository.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	account := &accounts.User{Email: "initialized@example.invalid", Password: "hash"}
	if err := repository.Create(ctx, account); err != nil {
		t.Fatal(err)
	}
	before, err := repository.GetByID(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	metadata := accounts.TokenMetadata{AccountID: account.ID, TokenHash: "stored-hash", Purpose: accounts.ResetTokenPurpose, ExpiresAt: time.Now().UTC().Add(time.Hour)}
	if err := repository.InsertToken(ctx, metadata); err != nil {
		t.Fatal(err)
	}

	if err := repository.Initialize(ctx); err != nil {
		t.Fatal("repeated initialization:", err)
	}
	stored, err := repository.GetByID(ctx, account.ID)
	if err != nil || !reflect.DeepEqual(stored, before) {
		t.Fatalf("repeated initialization changed account: %+v, %v", stored, err)
	}
	token, err := repository.GetToken(ctx, metadata.TokenHash, metadata.Purpose)
	if err != nil || token.AccountID != metadata.AccountID || token.TokenHash != metadata.TokenHash || token.Purpose != metadata.Purpose || !token.ExpiresAt.Equal(metadata.ExpiresAt) {
		t.Fatalf("repeated initialization changed token: %+v, %v", token, err)
	}
	if err := db.Ping(); err != nil {
		t.Fatal("provider closed its borrowed pool:", err)
	}
}

func TestSQLiteAccountRepository(t *testing.T) {
	for _, by := range []string{"ID", "email"} {
		t.Run(by, func(t *testing.T) {
			db := userTestDB(t)
			reader := NewAccountRepository(db)
			account := &accounts.User{Email: "reader@example.invalid", Password: "hash", Verified: false, VerificationRequired: true, RegistrationAuthType: "email"}
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
			if err != nil || stored.ID != account.ID || stored.Email != account.Email || stored.Password != account.Password || stored.Verified || !stored.VerificationRequired || stored.RegistrationAuthType != "email" || stored.SessionVersion != 1 || !stored.CreatedAt.Equal(account.CreatedAt) || !stored.UpdatedAt.Equal(account.UpdatedAt) {
				t.Fatalf("account fields were lost: %+v, %v", stored, err)
			}
			again, err := lookup(context.Background(), key)
			if err != nil || !reflect.DeepEqual(stored, again) || stored == again {
				t.Fatal("reads must return independent account values:", err)
			}
			var notFound *accounts.ErrUserNotFound
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

func TestSQLiteAccountRepositoryCaseInsensitiveEmail(t *testing.T) {
	repository := NewAccountRepository(userTestDB(t))
	for _, emails := range [][]string{
		{"Person@Example.INVALID", "person@example.invalid", "PERSON@EXAMPLE.INVALID"},
		{"Élodie@EXAMPLE.invalid", "élodie@example.INVALID"},
		{"σ@example.invalid", "ς@example.invalid", "Σ@EXAMPLE.INVALID"},
	} {
		account := &accounts.User{Email: emails[0]}
		if err := repository.Create(t.Context(), account); err != nil {
			t.Fatal(err)
		}
		for _, email := range emails {
			stored, err := repository.GetByEmail(t.Context(), email)
			if err != nil || stored.ID != account.ID || stored.Email != emails[0] {
				t.Fatalf("case-insensitive lookup = %+v, %v", stored, err)
			}
			duplicate := accounts.User{Email: email}
			before := duplicate
			if err := repository.Create(t.Context(), &duplicate); !errors.Is(err, accounts.ErrEmailAlreadyExists) || duplicate != before {
				t.Fatalf("duplicate email changed its input or returned %v", err)
			}
		}
	}

	for _, email := range []string{"person+tag@example.invalid", "per.son@example.invalid", " person@example.invalid"} {
		var missing *accounts.ErrUserNotFound
		if stored, err := repository.GetByEmail(t.Context(), email); stored != nil || !errors.As(err, &missing) {
			t.Fatalf("nonmatching lookup = %+v, %v", stored, err)
		}
	}
}

func TestSQLiteAccountRepositoryConcurrentCreate(t *testing.T) {
	db := userTestDB(t)
	repository := NewAccountRepository(db)
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, email := range []string{"Shared@example.invalid", "shared@EXAMPLE.INVALID"} {
		go func() {
			<-start
			account := &accounts.User{Email: email}
			results <- repository.Create(t.Context(), account)
		}()
	}
	close(start)

	var successes, duplicates int
	for range 2 {
		err := <-results
		switch {
		case err == nil:
			successes++
		case errors.Is(err, accounts.ErrEmailAlreadyExists):
			duplicates++
		default:
			t.Errorf("create failed: %v", err)
		}
	}
	if successes != 1 || duplicates != 1 {
		t.Fatalf("successes = %d, duplicates = %d", successes, duplicates)
	}
	var count int
	if err := db.QueryRowContext(t.Context(), "SELECT count(*) FROM user").Scan(&count); err != nil || count != 1 {
		t.Fatalf("stored accounts = %d, %v", count, err)
	}
}

func TestSQLiteAccountRepositoryCreateFailure(t *testing.T) {
	for _, scenario := range []string{"duplicate", "nil pool", "closed pool", "canceled", "write failure"} {
		t.Run(scenario, func(t *testing.T) {
			db := userTestDB(t)
			repository := NewAccountRepository(db)
			account := &accounts.User{Email: "new@example.invalid", Password: "new hash", VerificationRequired: true}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var existing *accounts.User
			switch scenario {
			case "duplicate":
				u := &accounts.User{Email: account.Email, Password: "original hash", Verified: true}
				if err := repository.Create(ctx, u); err != nil {
					t.Fatal(err)
				}
				var err error
				existing, err = repository.GetByID(ctx, u.ID)
				if err != nil {
					t.Fatal(err)
				}
			case "nil pool":
				repository = NewAccountRepository(nil)
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
			if errors.Is(err, accounts.ErrEmailAlreadyExists) != (scenario == "duplicate") {
				t.Fatalf("incorrect duplicate-email classification: %v", err)
			}
			if existing != nil {
				stored, err := repository.GetByID(context.Background(), existing.ID)
				if err != nil || !reflect.DeepEqual(stored, existing) {
					t.Fatal("failed insertion changed an existing account:", err)
				}
			} else if scenario != "closed pool" {
				var count int
				if err := db.QueryRowContext(t.Context(), "SELECT count(*) FROM user").Scan(&count); err != nil || count != 0 {
					t.Fatalf("failed insertion left an account: count=%d, error=%v", count, err)
				}
			}
		})
	}
}

func TestSQLiteAccountRepositoryLookupCancellationWhileWaiting(t *testing.T) {
	for _, by := range []string{"ID", "email"} {
		t.Run(by, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				db := userTestDB(t)
				repository := NewAccountRepository(db)
				lookup := repository.GetByID
				if by == "email" {
					lookup = repository.GetByEmail
				}
				// Hold the only connection so the lookup must wait for its context.
				connection, err := db.Conn(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				defer connection.Close()
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()

				var account *accounts.User
				result := make(chan error, 1)
				go func() {
					var err error
					account, err = lookup(ctx, "waiting")
					result <- err
				}()
				synctest.Wait()
				select {
				case err := <-result:
					t.Fatalf("lookup did not wait for the connection: %v", err)
				default:
				}

				cancel()
				err = <-result
				var notFound *accounts.ErrUserNotFound
				if account != nil || !errors.Is(err, context.Canceled) || errors.As(err, &notFound) {
					t.Fatalf("canceled lookup = %v, %v, want nil account and context.Canceled", account, err)
				}
			})
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
		account := &accounts.User{Email: "waiting@example.invalid"}
		before := *account
		result := make(chan error, 1)
		go func() { result <- NewAccountRepository(db).Create(ctx, account) }()
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
		if err := db.QueryRowContext(t.Context(), "SELECT count(*) FROM user").Scan(&count); err != nil || count != 0 {
			t.Fatalf("canceled insertion left an account: count=%d, error=%v", count, err)
		}
	})
}

func TestSQLiteAccountRepositoryRequiresPreparedStorage(t *testing.T) {
	for _, reader := range []*AccountRepository{NewAccountRepository(nil), NewAccountRepository(unpreparedUserTestDB(t))} {
		for _, lookup := range []func(context.Context, string) (*accounts.User, error){reader.GetByID, reader.GetByEmail} {
			var notFound *accounts.ErrUserNotFound
			if got, err := lookup(context.Background(), "account"); got != nil || err == nil || errors.As(err, &notFound) {
				t.Fatalf("unavailable storage treated as missing account: %v, %v", got, err)
			}
		}
	}
}

func TestSQLiteAccountRepositoryConcurrentPasswordChanges(t *testing.T) {
	db := userTestDB(t)
	repository := NewAccountRepository(db)
	u := &accounts.User{Email: "change@example.invalid", Password: "old hash", RegistrationAuthType: "email"}
	if err := repository.Create(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	u, err := repository.GetByID(context.Background(), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	before := *u
	accounts := [2]accounts.User{before, before}
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
		repository := NewAccountRepository(db)
		u := &accounts.User{Email: "change@example.invalid", Password: "old hash", RegistrationAuthType: "email"}
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
		repository := NewAccountRepository(db)
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
		if _, err := accounts.ValidateVerificationToken(context.Background(), NewAccountRepository(db), token.Token, key); err != nil {
			t.Fatalf("canceled update revoked the verification token: %v", err)
		}
	})
}
