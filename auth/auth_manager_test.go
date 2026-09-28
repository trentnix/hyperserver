package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLoginRouteUsesPathRatherThanHost(t *testing.T) {
	mux := http.NewServeMux()
	manager := &AuthManager{Enabled: true}
	manager.Routes(mux)
	for _, host := range []string{"localhost", "example.com"} {
		req := httptest.NewRequest(http.MethodGet, "http://"+host+"/auth/login", nil)
		_, pattern := mux.Handler(req)
		if pattern != "/auth/login" {
			t.Errorf("host %s: pattern = %q, want /auth/login", host, pattern)
		}
	}
}

func TestDisabledAuthDoesNotRegisterLogin(t *testing.T) {
	mux := http.NewServeMux()
	manager := &AuthManager{}
	manager.Routes(mux)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/auth/login", nil))
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}
