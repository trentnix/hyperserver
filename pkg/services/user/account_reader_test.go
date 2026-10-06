package user

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/services/session"
)

func TestSQLiteAccountReader(t *testing.T) {
	for _, by := range []string{"ID", "email"} {
		t.Run(by, func(t *testing.T) {
			db := userTestDB(t)
			account := &User{Email: "reader@example.invalid", Password: "hash", Verified: true, VerificationRequired: true, RegistrationAuthType: "email"}
			if err := account.Create(context.Background(), db); err != nil {
				t.Fatal(err)
			}
			reader := NewSQLiteAccountReader(db)
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

func TestSQLiteAccountReaderRequiresPreparedStorage(t *testing.T) {
	for _, reader := range []*SQLiteAccountReader{NewSQLiteAccountReader(nil), NewSQLiteAccountReader(unpreparedUserTestDB(t))} {
		for _, lookup := range []func(context.Context, string) (*User, error){reader.GetByID, reader.GetByEmail} {
			var notFound *ErrUserNotFound
			if got, err := lookup(context.Background(), "account"); got != nil || err == nil || errors.As(err, &notFound) {
				t.Fatalf("unavailable storage treated as missing account: %v, %v", got, err)
			}
		}
	}
}

type accountReaderStub struct {
	AccountReader
	lookup func(context.Context, string) (*User, error)
}

func (s accountReaderStub) GetByID(ctx context.Context, id string) (*User, error) {
	return s.lookup(ctx, id)
}

func TestGetAuthenticatedUserUsesAccountReader(t *testing.T) {
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
