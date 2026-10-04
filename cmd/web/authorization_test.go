package main

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/trentnix/hyperserver/pkg/services/middleware"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

func TestHTTPAuthorization(t *testing.T) {
	runHTTPScenario(t, func(h *httpHarness) {
		owner := h.seedUser(t, "owner@example.invalid")
		other := h.seedUser(t, "other@example.invalid")
		policyCalls, handlerCalls := 0, 0
		permission := true
		var policyErr error
		policy := func(r *http.Request) (bool, error) {
			policyCalls++
			u := user.GetUserFromContext(r.Context())
			return permission && u != nil && !u.NeedsVerification() && u.ID == r.PathValue("owner"), policyErr
		}
		handler := middleware.RequireAuthorization(policy)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			handlerCalls++
			w.WriteHeader(http.StatusNoContent)
		}))
		h.app.Web.Handle("GET /authorization/{owner}", handler)
		h.app.Web.Handle("POST /authorization/{owner}", handler)

		check := func(want int) {
			t.Helper()
			for _, method := range []string{http.MethodGet, http.MethodPost} {
				for _, htmx := range []bool{false, true} {
					policyCalls, handlerCalls = 0, 0
					w := h.request(method, "/authorization/"+owner.ID, nil, htmx)
					wantHandlerCalls := 0
					if want == http.StatusNoContent {
						wantHandlerCalls = 1
					}
					if w.Code != want || policyCalls != 1 || handlerCalls != wantHandlerCalls {
						t.Fatalf("%s htmx=%t: status=%d policy calls=%d handler calls=%d, want %d 1 %d", method, htmx, w.Code, policyCalls, handlerCalls, want, wantHandlerCalls)
					}
					if strings.Contains(w.Body.String(), "private") || len(w.Result().Cookies()) != 0 || w.Header().Get("Location") != "" || w.Header().Get("HX-Redirect") != "" {
						t.Fatalf("unexpected authorization response: %v %s", w.Header(), w.Body.String())
					}
				}
			}
		}

		check(http.StatusForbidden) // Anonymous requests do not satisfy this policy.
		h.establishSession(t, other)
		check(http.StatusForbidden) // Authentication alone does not grant access.
		h.establishSession(t, owner)
		check(http.StatusNoContent)

		permission = false
		check(http.StatusForbidden) // Reevaluate policy with the same session.
		permission = true

		policyErr = errors.New("private authorization storage failure")
		check(http.StatusInternalServerError)
		if !strings.Contains(h.logs.String(), policyErr.Error()) {
			t.Fatal("policy failure was not logged")
		}
		policyErr = nil
		check(http.StatusNoContent)

		owner.Verified, owner.VerificationRequired = false, true
		if err := owner.Update(context.Background(), h.app.Database); err != nil {
			t.Fatal(err)
		}
		// Account-security changes revoke existing sessions.
		h.establishSession(t, owner)
		check(http.StatusForbidden) // This policy also requires account verification.
		owner.Verified = true
		if err := owner.Update(context.Background(), h.app.Database); err != nil {
			t.Fatal(err)
		}
		h.establishSession(t, owner)
		check(http.StatusNoContent)

		// Identity-loading failures must stop before authorization or handler work.
		if _, err := h.app.Database.Exec(`ALTER TABLE user RENAME TO unavailable_user`); err != nil {
			t.Fatal(err)
		}
		for _, htmx := range []bool{false, true} {
			policyCalls, handlerCalls = 0, 0
			w := h.request(http.MethodPost, "/authorization/"+owner.ID, nil, htmx)
			if w.Code != http.StatusInternalServerError || policyCalls != 0 || handlerCalls != 0 {
				t.Fatalf("identity-loading failure reached authorization: status=%d policy calls=%d handler calls=%d", w.Code, policyCalls, handlerCalls)
			}
		}
	})
}
