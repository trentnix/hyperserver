package session_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

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
			session.WithSessionManager(manager)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

func TestWithSessionManagerPreservesRequestContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	originalManager, manager := &session.SessionManager{}, &session.SessionManager{}
	identity := &user.User{ID: "existing-user"}
	r := httptest.NewRequestWithContext(ctx, http.MethodGet, "/", nil)
	r = user.AddUserToRequestContext(r, identity)
	r = session.AddSessionManagerToRequestContext(r, originalManager)

	called := false
	handler := session.WithSessionManager(manager)(http.HandlerFunc(func(w http.ResponseWriter, got *http.Request) {
		called = true
		if session.GetSessionManager(got) != manager || user.GetUserFromContext(got.Context()) != identity || got.Context().Err() != context.Canceled {
			t.Fatal("attaching a session manager lost request context or used the wrong manager")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if !called || w.Code != http.StatusNoContent || len(w.Result().Cookies()) != 0 {
		t.Fatalf("called=%t status=%d cookies=%v", called, w.Code, w.Result().Cookies())
	}
	if session.GetSessionManager(r) != originalManager {
		t.Fatal("middleware replaced the original request's session manager")
	}
}
