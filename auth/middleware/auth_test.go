package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/trentnix/hyperserver/pkg/services/content"
	"github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

func TestAuthMiddlewareMissingDependencies(t *testing.T) {
	for name, guard := range map[string]func(user.AccountRepository, *content.ContentManagerService) func(http.Handler) http.Handler{
		"authenticated": RequireAuthentication,
		"anonymous":     RequireAnonymous,
	} {
		for _, missing := range []string{"account repository", "content manager", "both"} {
			t.Run(name+"/"+missing, func(t *testing.T) {
				// A cached user avoids repository calls if a missing return lets execution continue.
				r := user.AddUserToRequestContext(httptest.NewRequest(http.MethodGet, "/private", nil), &user.User{ID: "user", Verified: true})
				var accounts user.AccountRepository = &struct{ user.AccountRepository }{}
				cm := &content.ContentManagerService{HandleError: func(http.ResponseWriter, *http.Request, string, error, int) {
					t.Error("authentication continued after a dependency error")
				}}
				if missing == "account repository" || missing == "both" {
					accounts = nil
				}
				if missing == "content manager" || missing == "both" {
					cm = nil
				}
				w := httptest.NewRecorder()
				guard(accounts, cm)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
					t.Error("downstream handler ran with a missing dependency")
				})).ServeHTTP(w, r)
				if w.Code != http.StatusInternalServerError {
					t.Fatalf("status = %d, want 500", w.Code)
				}
				want := "content manager not specified\n"
				if accounts == nil {
					want = "account repository is not configured\n"
				}
				if w.Body.String() != want {
					t.Fatalf("body = %q, want %q", w.Body.String(), want)
				}
				if len(w.Result().Cookies()) != 0 || w.Header().Get("HX-Redirect") != "" {
					t.Fatal("dependency failure changed cookies or redirected")
				}
			})
		}
	}
}

func TestSessionMiddlewareMissingDependencies(t *testing.T) {
	for _, missing := range []string{"account repository", "session manager", "both"} {
		t.Run(missing, func(t *testing.T) {
			var accounts user.AccountRepository = &struct{ user.AccountRepository }{}
			manager := &session.SessionManager{}
			if missing == "account repository" || missing == "both" {
				accounts = nil
			}
			if missing == "session manager" || missing == "both" {
				manager = nil
			}
			r := httptest.NewRequest(http.MethodGet, "/private", nil)
			w := httptest.NewRecorder()
			LoadSessionManagement(accounts, manager)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				t.Error("downstream handler ran with a missing dependency")
			})).ServeHTTP(w, r)
			if w.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want 500", w.Code)
			}
			if got, err := loadAuthenticatedUser(r, nil); got != r || err == nil || err.Error() != "account repository is not configured" {
				t.Fatalf("missing repository: request preserved=%t error=%v", got == r, err)
			}
		})
	}
}
