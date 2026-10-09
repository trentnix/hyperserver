package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/trentnix/hyperserver/pkg/services/content"
	"github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

func TestAuthMiddlewareMissingDependencies(t *testing.T) {
	for name, guard := range map[string]func(user.AccountRepository, *content.ContentManagerService) func(http.Handler) http.Handler{
		"authenticated": RequireAuthentication,
		"anonymous":     RequireAnonymous,
	} {
		for _, missing := range []string{"database", "content manager", "both"} {
			t.Run(name+"/"+missing, func(t *testing.T) {
				// A cached user avoids database I/O if a missing return lets execution continue.
				r := user.AddUserToRequestContext(httptest.NewRequest(http.MethodGet, "/private", nil), &user.User{ID: "user", Verified: true})
				var db user.AccountRepository = user.NewSQLiteAccountRepository(&sqlx.DB{})
				cm := &content.ContentManagerService{HandleError: func(http.ResponseWriter, *http.Request, string, error, int) {
					t.Error("authentication continued after a dependency error")
				}}
				if missing == "database" || missing == "both" {
					db = nil
				}
				if missing == "content manager" || missing == "both" {
					cm = nil
				}
				w := httptest.NewRecorder()
				guard(db, cm)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
					t.Error("downstream handler ran with a missing dependency")
				})).ServeHTTP(w, r)
				if w.Code != http.StatusInternalServerError {
					t.Fatalf("status = %d, want 500", w.Code)
				}
				if len(w.Result().Cookies()) != 0 || w.Header().Get("HX-Redirect") != "" {
					t.Fatal("dependency failure changed cookies or redirected")
				}
			})
		}
	}
}

func TestSessionMiddlewareMissingDependencies(t *testing.T) {
	for _, missing := range []string{"database", "session manager", "both"} {
		t.Run(missing, func(t *testing.T) {
			var db user.AccountRepository = user.NewSQLiteAccountRepository(&sqlx.DB{})
			manager := &session.SessionManager{}
			if missing == "database" || missing == "both" {
				db = nil
			}
			if missing == "session manager" || missing == "both" {
				manager = nil
			}
			r := httptest.NewRequest(http.MethodGet, "/private", nil)
			w := httptest.NewRecorder()
			LoadSessionManagement(db, manager)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				t.Error("downstream handler ran with a missing dependency")
			})).ServeHTTP(w, r)
			if w.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want 500", w.Code)
			}
			if got, err := loadSessionManager(r, nil); got != r || err == nil {
				t.Fatal("missing session manager did not preserve the request and return an error")
			}
			if got, err := loadAuthenticatedUser(r, nil); got != r || err == nil {
				t.Fatal("missing database did not preserve the request and return an error")
			}
		})
	}
}

func TestWithSessionManager(t *testing.T) {
	for _, present := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing", true: "present"}[present], func(t *testing.T) {
			var manager *session.SessionManager
			if present {
				manager = &session.SessionManager{}
			}
			called := false
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			w := httptest.NewRecorder()
			WithSessionManager(manager)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				if session.GetSessionManager(r) != manager {
					t.Error("wrong session manager attached")
				}
				if user.GetUserFromContext(r.Context()) != nil {
					t.Error("session-only middleware loaded an identity")
				}
				w.WriteHeader(http.StatusNoContent)
			})).ServeHTTP(w, r)
			want := http.StatusInternalServerError
			if present {
				want = http.StatusNoContent
			}
			if called != present || w.Code != want {
				t.Fatalf("called = %t, status = %d", called, w.Code)
			}
			if session.GetSessionManager(r) != nil {
				t.Error("middleware changed the original request")
			}
			if len(w.Result().Cookies()) != 0 {
				t.Error("attaching the manager issued a cookie")
			}
		})
	}
}
