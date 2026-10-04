package middleware

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequireAuthorization(t *testing.T) {
	for _, tc := range []struct {
		name    string
		allowed bool
		err     error
		status  int
	}{
		{"allowed", true, nil, http.StatusNoContent},
		{"denied", false, nil, http.StatusForbidden},
		{"policy failure", false, errors.New("private storage failure"), http.StatusInternalServerError},
		{"error overrides allow", true, errors.New("private storage failure"), http.StatusInternalServerError},
		{"canceled", false, context.Canceled, http.StatusInternalServerError},
		{"missing policy", false, nil, http.StatusInternalServerError},
	} {
		for _, htmx := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/htmx=%t", tc.name, htmx), func(t *testing.T) {
				r := httptest.NewRequest(http.MethodPost, "/protected", nil)
				if htmx {
					r.Header.Set("HX-Request", "true")
				}
				ctx, cancel := context.WithCancel(r.Context())
				defer cancel()
				r = r.WithContext(ctx)
				if tc.err == context.Canceled {
					cancel()
				}
				policyCalls, handlerCalls := 0, 0
				policy := func(got *http.Request) (bool, error) {
					policyCalls++
					if got != r || got.Context() != ctx {
						t.Fatal("policy did not receive the original request and context")
					}
					if tc.err == context.Canceled {
						return false, got.Context().Err()
					}
					return tc.allowed, tc.err
				}
				if tc.name == "missing policy" {
					policy = nil
				}
				handler := RequireAuthorization(policy)(http.HandlerFunc(func(w http.ResponseWriter, got *http.Request) {
					handlerCalls++
					if got != r {
						t.Fatal("handler did not receive the original request")
					}
					w.WriteHeader(http.StatusNoContent)
				}))
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, r)
				wantPolicyCalls, wantHandlerCalls := 1, 0
				if policy == nil {
					wantPolicyCalls = 0
				}
				if tc.status == http.StatusNoContent {
					wantHandlerCalls = 1
				}
				if w.Code != tc.status || policyCalls != wantPolicyCalls || handlerCalls != wantHandlerCalls {
					t.Fatalf("status=%d policy calls=%d handler calls=%d, want %d %d %d", w.Code, policyCalls, handlerCalls, tc.status, wantPolicyCalls, wantHandlerCalls)
				}
				if tc.status != http.StatusNoContent && w.Body.String() != http.StatusText(tc.status)+"\n" {
					t.Fatalf("error response = %q, want only the public status text", w.Body.String())
				}
				if strings.Contains(w.Body.String(), "private") || len(w.Result().Cookies()) != 0 || w.Header().Get("Location") != "" || w.Header().Get("HX-Redirect") != "" {
					t.Fatalf("unexpected response: %v %s", w.Header(), w.Body.String())
				}
			})
		}
	}
}
