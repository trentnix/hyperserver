package main

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/modules/site/models"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

func TestHTTPMixedStorage(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "contacts")
	catalog, err := siteModules(directory)
	if err != nil {
		t.Fatal(err)
	}
	runHTTPScenarioWithCatalog(t, 30*time.Second, catalog, func(h *httpHarness) {
		w := h.request(http.MethodPost, "/auth/register/email", registrationForm(), true)
		if w.Header().Get("HX-Redirect") != "/login" {
			t.Fatalf("SQL-backed registration failed: %d %s", w.Code, w.Body.String())
		}
		account, err := user.GetUserByEmail(h.app.Database, registrationForm().Get("email"))
		if err != nil {
			t.Fatal(err)
		}
		var count int
		if err := h.app.Database.Get(&count, `SELECT count(*) FROM sqlite_master WHERE name = 'hyperserver_contact_submission'`); err != nil || count != 0 {
			t.Fatalf("file-backed module prepared SQL storage: count=%d, error=%v", count, err)
		}
		// Contacts remain usable even when the SQL account table is unavailable.
		if _, err := h.app.Database.Exec(`ALTER TABLE user RENAME TO unavailable_user`); err != nil {
			t.Fatal(err)
		}
		form := url.Values{"name": {"Person"}, "email": {"person@example.invalid"}, "message": {"File-backed contact"}}
		for _, htmx := range []bool{false, true} {
			w = h.request(http.MethodPost, "/contact", form, htmx)
			if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Your message has been submitted.") {
				t.Fatalf("file-backed contact failed: %d %s", w.Code, w.Body.String())
			}
		}
		entries, err := os.ReadDir(directory)
		if err != nil || len(entries) != 2 {
			t.Fatalf("stored contact files = %d, %v", len(entries), err)
		}
		for _, entry := range entries {
			data, err := os.ReadFile(filepath.Join(directory, entry.Name()))
			if err != nil {
				t.Fatal(err)
			}
			var stored models.ContactSubmission
			if err := json.Unmarshal(data, &stored); err != nil {
				t.Fatal(err)
			}
			if stored.Email != form.Get("email") || stored.Message != form.Get("message") || entry.Name() != stored.Id+".json" {
				t.Fatalf("incorrect contact file: %+v", stored)
			}
		}
		if _, err := h.app.Database.Exec(`ALTER TABLE unavailable_user RENAME TO user`); err != nil {
			t.Fatal(err)
		}
		// Losing file storage does not prevent SQL account access or writes.
		if err := os.Rename(directory, directory+"-offline"); err != nil {
			t.Fatal(err)
		}
		w = h.request(http.MethodPost, "/contact", form, true)
		if strings.Contains(w.Body.String(), "Your message has been submitted.") || !strings.Contains(w.Body.String(), "Unable to save your message") {
			t.Fatalf("file failure was not reported: %d %s", w.Code, w.Body.String())
		}
		h.assertUserUnchanged(t, account)
		form = registrationForm()
		form.Set("email", "another@example.invalid")
		w = h.request(http.MethodPost, "/auth/register/email", form, true)
		if w.Header().Get("HX-Redirect") != "/login" {
			t.Fatalf("file failure affected SQL registration: %d %s", w.Code, w.Body.String())
		}
	}, func(cfg *config.Config) {
		cfg.Auth.RegisterRequiresVerification = false
	})
}
