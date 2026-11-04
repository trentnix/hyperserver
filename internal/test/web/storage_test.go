package web_test

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/handlers"
)

// A module's storage need not use the authentication database.
func TestHTTPMixedStorage(t *testing.T) {
	var stored []contactSubmission
	var storageErr error
	catalog := func(h *httpHarness) ([]handlers.Descriptor, error) {
		return siteCatalog(h, func(ctx context.Context, submission *contactSubmission) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if storageErr != nil {
				return storageErr
			}
			stored = append(stored, *submission)
			return nil
		})
	}
	runSiteScenarioWithCatalog(t, catalog, func(h *httpHarness) {
		w := h.request(http.MethodPost, "/register", registrationForm(), true)
		if w.Header().Get("HX-Redirect") != "/login" {
			t.Fatalf("SQL-backed registration failed: %d %s", w.Code, w.Body.String())
		}
		account, err := h.accounts.GetByEmail(t.Context(), registrationForm().Get("email"))
		if err != nil {
			t.Fatal(err)
		}
		var count int
		if err := h.db.QueryRowContext(t.Context(), `SELECT count(*) FROM sqlite_master WHERE name = 'hyperserver_contact_submission'`).Scan(&count); err != nil || count != 0 {
			t.Fatalf("injected writer prepared SQL storage: count=%d, error=%v", count, err)
		}
		if _, err := h.db.Exec(`ALTER TABLE user RENAME TO unavailable_user`); err != nil {
			t.Fatal(err)
		}
		values := url.Values{"name": {"Person"}, "email": {"person@example.invalid"}, "message": {"Independent storage"}}
		for _, htmx := range []bool{false, true} {
			w = h.request(http.MethodPost, "/contact", values, htmx)
			if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Your message has been submitted.") {
				t.Fatalf("independent write failed: %d %s", w.Code, w.Body.String())
			}
		}
		if len(stored) != 2 {
			t.Fatalf("submissions = %d, want 2", len(stored))
		}
		for _, submission := range stored {
			if submission.Email != values.Get("email") || submission.Message != values.Get("message") {
				t.Fatalf("incorrect submission: %+v", submission)
			}
		}
		if _, err := h.db.Exec(`ALTER TABLE unavailable_user RENAME TO user`); err != nil {
			t.Fatal(err)
		}
		storageErr = errors.New("module storage unavailable")
		w = h.request(http.MethodPost, "/contact", values, true)
		if strings.Contains(w.Body.String(), "Your message has been submitted.") || !strings.Contains(w.Body.String(), "Unable to save your message") {
			t.Fatalf("storage failure was not reported: %d %s", w.Code, w.Body.String())
		}
		h.assertUserUnchanged(t, account)
		values = registrationForm()
		values.Set("email", "another@example.invalid")
		w = h.request(http.MethodPost, "/register", values, true)
		if w.Header().Get("HX-Redirect") != "/login" {
			t.Fatalf("module failure affected SQL registration: %d %s", w.Code, w.Body.String())
		}
	}, func(cfg *config.Config) { cfg.Auth.RegisterRequiresVerification = false })
}
