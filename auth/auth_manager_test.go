package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/trentnix/hyperserver/pkg/database"
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

func TestRegistrationReportsDatabaseFailure(t *testing.T) {
	err, message := ProcessRegistration(context.Background(), nil, "person@example.invalid", "", "email", true)
	var unavailable *database.ErrDatabaseUnavailable
	if !errors.As(err, &unavailable) || message != "There was an error creating a user account" {
		t.Fatalf("database failure = (%v, %q), want database unavailable and creation failure", err, message)
	}
}

func TestRegistrationHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err, message := ProcessRegistration(ctx, nil, "person@example.invalid", "TestPassword1!", "email", true)
	if !errors.Is(err, context.Canceled) || message != "There was an error creating a user account" {
		t.Fatalf("canceled registration = (%v, %q)", err, message)
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
