package blog

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/trentnix/hyperserver/pkg/services/user"
)

func TestMultiUserConfiguration(t *testing.T) {
	for _, setting := range []string{"", "multiUser: false\n", "multiUser: true\n"} {
		t.Run(strings.TrimSpace(setting), func(t *testing.T) {
			cfg := harnessConfig(t, 10)
			path := filepath.Join(cfg.App.WorkingDirectory, "modules/blog/config.yaml")
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			data = []byte(strings.Replace(string(data), "multiUser: true\n", setting, 1))
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			h := startHarness(t, cfg)
			requireStatus(t, h.request("GET", "/register", nil, false), http.StatusOK)
			h.register(t)
			h.verify(t)
			h.login(t)
			requireStatus(t, h.request("GET", "/profile", nil, false), http.StatusSeeOther)
			enabled := setting == "multiUser: true\n"
			for _, hx := range []bool{false, true} {
				response := h.request("GET", "/profile", nil, hx)
				requireStatus(t, response, http.StatusOK)
				if !hx && strings.Contains(response.Body.String(), `href="/users"`) != enabled {
					t.Fatal("Users navigation does not match configuration")
				}
				if enabled {
					requireStatus(t, h.request("GET", "/users", nil, hx), http.StatusOK)
					continue
				}
				users := h.request("GET", "/users", nil, hx)
				requireStatus(t, users, http.StatusOK)
				if strings.Contains(users.Body.String(), `href="/users/add"`) {
					t.Fatal("disabled invitations remain visible")
				}
				for _, route := range []struct{ method, path string }{
					{"GET", "/users/add"}, {"POST", "/users"},
					{"GET", "/users/unused"}, {"POST", "/users/unused"},
					{"POST", "/users/unused/invite"}, {"GET", "/users/unused/remove"}, {"POST", "/users/unused/remove"},
					{"GET", "/invite?token=unused"}, {"POST", "/invite/dismiss"},
					{"GET", "/invite/accept"}, {"POST", "/invite/accept"},
				} {
					requireStatus(t, h.request(route.method, route.path, url.Values{"email": {"writer@example.test"}, "role": {"poster"}}, hx), http.StatusNotFound)
				}
			}
			if enabled {
				inviteUserForTest(t, h, "writer@example.test", "poster")
			} else if len(h.mail.messages) != 1 {
				t.Fatal("disabled user management sent mail")
			}
		})
	}
}

func TestDisableMultiUserPreservesRegisteredAccounts(t *testing.T) {
	cfg := harnessConfig(t, 10)
	h := startHarness(t, cfg)
	h.register(t)
	h.verify(t)
	h.login(t)
	requireStatus(t, h.request("GET", "/profile", nil, false), http.StatusSeeOther)
	adminCookies := h.cookies
	poster := registeredPoster(t, h)
	posterCookies := h.cookies
	h.cookies = adminCookies
	link := inviteUserForTest(t, h, "pending@example.test", "poster")
	pending, err := h.app.Accounts.GetByEmail(t.Context(), "pending@example.test")
	if err != nil {
		t.Fatal(err)
	}
	if err := h.app.ShutdownContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(cfg.App.WorkingDirectory, "modules/blog/config.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(data), "multiUser: true", "multiUser: false", 1)), 0600); err != nil {
		t.Fatal(err)
	}
	h = startHarness(t, cfg)
	h.cookies = adminCookies
	listing := h.request("GET", "/users", nil, false)
	requireStatus(t, listing, http.StatusOK)
	if !strings.Contains(listing.Body.String(), `href="/users"`) || strings.Contains(listing.Body.String(), `href="/users/add"`) {
		t.Fatal("disabled multi-user mode must keep management without invitations")
	}
	for _, hx := range []bool{false, true} {
		editor := h.request("GET", "/users/"+pending.ID, nil, hx)
		requireStatus(t, editor, http.StatusOK)
		if strings.Contains(editor.Body.String(), "Resend invitation") {
			t.Fatal("disabled multi-user mode offers resend")
		}
		requireStatus(t, h.request("POST", "/users/"+pending.ID+"/invite", url.Values{"version": {strconv.FormatInt(pending.SessionVersion, 10)}}, hx), http.StatusNotFound)
	}
	h.cookies = posterCookies
	requireStatus(t, h.request("GET", "/post/create", nil, false), http.StatusOK)
	h.cookies = make(map[string]*http.Cookie)
	// Even a retained invitation cookie cannot restore registration access.
	invitationURL, err := url.Parse(link)
	if err != nil {
		t.Fatal(err)
	}
	h.cookies[invitationCookie] = &http.Cookie{Name: invitationCookie, Value: invitationURL.Query().Get("token")}
	for _, hx := range []bool{false, true} {
		requireStatus(t, h.request("GET", link, nil, hx), http.StatusNotFound)
		response := h.request("GET", "/register", nil, hx)
		if hx {
			if response.Header().Get("HX-Redirect") != loginURL {
				t.Fatal("disabled registration did not redirect to login")
			}
		} else {
			requireStatus(t, response, http.StatusSeeOther)
		}
		h.request("POST", "/register", credentials(pending.Email), hx)
	}
	if allowed, err := h.blog.access.AllowAccount(t.Context(), pending.Email); err != nil || allowed {
		t.Fatal("unregistered invitation still grants admission", err)
	}
	stored, err := h.app.Accounts.GetByID(t.Context(), pending.ID)
	if err != nil || *stored != *pending {
		t.Fatal("disabled invitation changed account state", err)
	}
	requireStatus(t, h.request("POST", "/login", credentials(poster.Email), false), http.StatusSeeOther)
	requireStatus(t, h.request("GET", "/post/create", nil, false), http.StatusOK)
	if _, err := user.ValidateInvitation(t.Context(), h.app.Accounts, invitationURL.Query().Get("token")); err != nil {
		t.Fatal("disabling consumed the invitation", err)
	}
	// Existing users remain manageable, including promoting and removing another admin.
	h.cookies = adminCookies
	requireStatus(t, h.request("POST", "/users/"+poster.ID, url.Values{"version": {strconv.FormatInt(poster.SessionVersion, 10)}, "role": {"admin"}}, false), http.StatusSeeOther)
	poster, err = h.app.Accounts.GetByID(t.Context(), poster.ID)
	if err != nil || poster.Role != "admin" {
		t.Fatal("disabled multi-user mode prevented role changes", err)
	}
	requireStatus(t, h.request("POST", "/users/"+poster.ID+"/remove", url.Values{"version": {strconv.FormatInt(poster.SessionVersion, 10)}}, false), http.StatusSeeOther)
	if err := h.app.ShutdownContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	h = startHarness(t, cfg)
	requireStatus(t, h.request("GET", link, nil, false), http.StatusSeeOther)
	requireStatus(t, h.request("POST", "/register", credentials(pending.Email), false), http.StatusSeeOther)
	stored, err = h.app.Accounts.GetByID(t.Context(), pending.ID)
	if err != nil || stored.Invited || !stored.Verified || stored.RegistrationAuthType == "" {
		t.Fatal("re-enabling did not restore invitation registration", err)
	}
	if err := h.app.ShutdownContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(data), "multiUser: true", "multiUser: false", 1)), 0600); err != nil {
		t.Fatal(err)
	}
	h = startHarness(t, cfg)
	h.cookies = adminCookies
	requireStatus(t, h.request("POST", "/users/"+stored.ID+"/remove", url.Values{"version": {strconv.FormatInt(stored.SessionVersion, 10)}}, false), http.StatusSeeOther)
	response := h.request("GET", "/profile", nil, false)
	requireStatus(t, response, http.StatusOK)
	if strings.Contains(response.Body.String(), `href="/users"`) {
		t.Fatal("Users link remains after removing all other users")
	}
}

func TestAdminCannotRemoveSelf(t *testing.T) {
	h := usersHarness(t)
	adminCookies := h.cookies
	other := registeredPoster(t, h)
	h.cookies = adminCookies
	admin, err := h.app.Accounts.GetByEmail(t.Context(), "trentnix@gmail.com")
	if err != nil {
		t.Fatal(err)
	}
	other.Role = "admin"
	if err := h.blog.administration.ChangeUserRole(t.Context(), admin.ID, "admin", other); err != nil {
		t.Fatal(err)
	}
	path := "/users/" + admin.ID
	for _, hx := range []bool{false, true} {
		editor := h.request("GET", path, nil, hx)
		requireStatus(t, editor, http.StatusOK)
		if strings.Contains(editor.Body.String(), path+"/remove") {
			t.Fatal("admin sees a self-removal control")
		}
		requireStatus(t, h.request("GET", path+"/remove", nil, hx), http.StatusForbidden)
		requireStatus(t, h.request("POST", path+"/remove", url.Values{"version": {strconv.FormatInt(admin.SessionVersion, 10)}}, hx), http.StatusForbidden)
	}
	stored, err := h.app.Accounts.GetByID(t.Context(), admin.ID)
	if err != nil || stored.Role != "admin" || stored.SessionVersion != admin.SessionVersion {
		t.Fatal("self-removal changed access", err)
	}
	requireStatus(t, h.request("POST", "/users/"+other.ID+"/remove", url.Values{"version": {strconv.FormatInt(other.SessionVersion, 10)}}, false), http.StatusSeeOther)
}
