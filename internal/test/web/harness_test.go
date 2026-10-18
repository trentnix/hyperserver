package web_test

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/trentnix/hyperserver/pkg/services/user"
)

func TestHTTPHarnessIsolation(t *testing.T) {
	assertHarnessIsolation(t, newHTTPHarness(t), newHTTPHarness(t))
}

func TestSiteHTTPHarnessIsolation(t *testing.T) {
	assertHarnessIsolation(t, newSiteHTTPHarness(t, nil), newSiteHTTPHarness(t, nil))
}

func assertHarnessIsolation(t *testing.T, first, second *httpHarness) {
	t.Helper()
	u := first.seedUser(t, "isolated@example.invalid")
	if _, err := second.accounts.GetByEmail(t.Context(), u.Email); !errors.Is(err, user.ErrUserNotFound) {
		t.Fatalf("second application lookup = %v, want missing user", err)
	}

	form := url.Values{"email": {u.Email}}
	first.request(http.MethodPost, "/auth/reset/request/email", form, false)
	second.request(http.MethodPost, "/auth/reset/request/email", form, false)
	if len(first.mail.snapshot()) != 1 || len(second.mail.snapshot()) != 0 {
		t.Fatal("applications did not deliver mail independently")
	}
	first.assertRowCount(t, "usertoken", 1)
	second.assertRowCount(t, "usertoken", 0)

	if err := first.app.Shutdown(); err != nil {
		t.Fatal(err)
	}
	if err := first.db.PingContext(t.Context()); err == nil {
		t.Fatal("shutdown left account storage open")
	}
	if err := second.db.PingContext(t.Context()); err != nil {
		t.Fatalf("shutting down the first application affected the second: %v", err)
	}
}

func TestHTTPHarnessLayouts(t *testing.T) {
	h := newHTTPHarness(t)
	for _, htmx := range []bool{false, true} {
		w := h.request(http.MethodGet, "/auth/reset/request/email", nil, htmx)
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "<form") {
			t.Fatalf("htmx=%t: status=%d body=%s", htmx, w.Code, w.Body.String())
		}
		if fullPage := strings.Contains(w.Body.String(), "<!doctype html>"); fullPage == htmx {
			t.Errorf("htmx=%t: full page=%t", htmx, fullPage)
		}
	}
}
