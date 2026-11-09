package blog

import (
	"context"
	"errors"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/trentnix/hyperserver/pkg/services/user"
)

func usersHarness(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t)
	h.register(t)
	h.verify(t)
	h.login(t)
	requireStatus(t, h.request("GET", "/users", nil, false), http.StatusOK)
	return h
}

func TestReinviteWithoutRequiredVerification(t *testing.T) {
	cfg := harnessConfig(t, 10)
	cfg.Auth.RegisterRequiresVerification = false
	h := startHarness(t, cfg)
	if err := h.blog.access.setup.finish(t.Context()); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, h.request("POST", "/register", credentials("trentnix@gmail.com"), false), http.StatusSeeOther)
	h.login(t)
	adminCookies := h.cookies
	inviteUserForTest(t, h, "writer@example.test", "poster")
	h.cookies = make(map[string]*http.Cookie)
	requireStatus(t, h.request("POST", "/register", credentials("writer@example.test"), false), http.StatusSeeOther)
	requireStatus(t, h.request("POST", "/login", credentials("writer@example.test"), false), http.StatusSeeOther)
	requireStatus(t, h.request("GET", "/post/create", nil, false), http.StatusOK)
	writer, err := h.app.Accounts.GetByEmail(t.Context(), "writer@example.test")
	if err != nil {
		t.Fatal(err)
	}
	h.cookies = adminCookies
	requireStatus(t, h.request("POST", "/users/"+writer.ID+"/remove", url.Values{"version": {strconv.FormatInt(writer.SessionVersion, 10)}}, false), http.StatusSeeOther)
	inviteUserForTest(t, h, writer.Email, "poster")
	h.cookies = make(map[string]*http.Cookie)
	response := h.request("POST", "/login", credentials(writer.Email), false)
	requireStatus(t, response, http.StatusSeeOther)
	if response.Header().Get("Location") == "/auth/request/verify" {
		t.Fatal("reinvitation enabled verification")
	}
	requireStatus(t, h.request("GET", "/post/create", nil, false), http.StatusOK)
	writer, err = h.app.Accounts.GetByEmail(t.Context(), writer.Email)
	if err != nil || writer.Invited || writer.Verified || writer.VerificationRequired {
		t.Fatal("acceptance changed verification policy or claimed email proof", err)
	}
}

func TestPosterSearchDoesNotLoadOtherAuthors(t *testing.T) {
	h := usersHarness(t)
	admin, err := h.app.Accounts.GetByEmail(t.Context(), "trentnix@gmail.com")
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := h.blog.persistPost(t.Context(), Post{Title: "Private", Draft: true, AuthorID: admin.ID, Markdown: "Other author's private body"})
	if err != nil {
		t.Fatal(err)
	}
	poster := registeredPoster(t, h)
	own, err := h.blog.persistPost(t.Context(), Post{Title: "My writing", Draft: true, AuthorID: poster.ID, Markdown: "Find this unique phrase"})
	if err != nil {
		t.Fatal(err)
	}
	response := h.request("GET", "/post/manage?q=unique+phrase", nil, false)
	requireStatus(t, response, http.StatusOK)
	if !strings.Contains(response.Body.String(), own.Title) || strings.Contains(response.Body.String(), foreign.Title) {
		t.Fatal("search did not restrict matches to the author")
	}
	repo := h.blog.posts.(*sqliteRepository)
	if _, loaded := repo.searchBodies.get(searchBodyKey{kind: "post", id: foreign.ID}, foreign.Version); loaded {
		t.Fatal("Poster search loaded another author's private body")
	}
	if _, loaded := repo.searchBodies.get(searchBodyKey{kind: "post", id: own.ID}, own.Version); !loaded {
		t.Fatal("search did not check the author's body")
	}
}

func inviteUserForTest(t *testing.T, h *harness, email, role string) string {
	t.Helper()
	requireStatus(t, h.request("POST", "/users", url.Values{"email": {email}, "role": {role}}, false), http.StatusSeeOther)
	message := h.mail.messages[len(h.mail.messages)-1].Body
	link := regexp.MustCompile(`https://blog.example/invite\?[^"< ]+`).FindString(message)
	parsed, err := url.Parse(html.UnescapeString(link))
	if err != nil || parsed.Query().Get("token") == "" {
		t.Fatalf("missing invitation: %s %v", message, err)
	}
	return parsed.RequestURI()
}

func TestInvitationRegistrationHTTP(t *testing.T) {
	for _, linked := range []bool{true, false} {
		t.Run(strconv.FormatBool(linked), func(t *testing.T) {
			h := usersHarness(t)
			link := inviteUserForTest(t, h, "writer@example.test", "poster")
			h.cookies = make(map[string]*http.Cookie)
			if linked {
				requireStatus(t, h.request("GET", link, nil, false), http.StatusSeeOther)
				u, _ := h.app.Accounts.GetByEmail(t.Context(), "writer@example.test")
				if u.Verified || !u.Invited {
					t.Fatal("GET accepted invitation")
				}
				page := h.request("GET", "/register", nil, false)
				requireStatus(t, page, http.StatusOK)
				if !strings.Contains(page.Body.String(), `value="writer@example.test"`) {
					t.Fatal("invitation email not prefilled")
				}
			}
			beforeMail := len(h.mail.messages)
			requireStatus(t, h.request("POST", "/register", credentials("writer@example.test"), false), http.StatusSeeOther)
			u, err := h.app.Accounts.GetByEmail(t.Context(), "writer@example.test")
			if err != nil || u.Verified != linked || u.Invited == linked || u.Role != "poster" {
				t.Fatalf("registration: %+v %v", u, err)
			}
			if linked && len(h.mail.messages) != beforeMail {
				t.Fatal("verified invitation sent redundant verification")
			}
			if !linked && len(h.mail.messages) != beforeMail+1 {
				t.Fatal("ordinary invited registration omitted verification")
			}
			requireStatus(t, h.request("POST", "/login", credentials(u.Email), false), http.StatusSeeOther)
			want := http.StatusOK
			if !linked {
				want = http.StatusForbidden
			}
			requireStatus(t, h.request("GET", "/post/create", nil, false), want)
			if !linked {
				// An authenticated, unverified recipient can accept the original link explicitly.
				requireStatus(t, h.request("GET", link, nil, false), http.StatusSeeOther)
				requireStatus(t, h.request("GET", "/invite/accept", nil, false), http.StatusOK)
				requireStatus(t, h.request("POST", "/invite/accept", nil, false), http.StatusSeeOther)
				requireStatus(t, h.request("GET", "/post/create", nil, false), http.StatusOK)
			}
			requireStatus(t, h.request("GET", link, nil, false), http.StatusBadRequest)
		})
	}
}

func TestInvitationWithoutRequiredVerification(t *testing.T) {
	cfg := harnessConfig(t, 10)
	cfg.Auth.RegisterRequiresVerification = false
	h := startHarness(t, cfg)
	if err := h.blog.access.setup.finish(t.Context()); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, h.request("POST", "/register", credentials("trentnix@gmail.com"), false), http.StatusSeeOther)
	h.login(t)
	link := inviteUserForTest(t, h, "writer@example.test", "poster")
	h.cookies = make(map[string]*http.Cookie)
	before := len(h.mail.messages)
	requireStatus(t, h.request("POST", "/register", credentials("writer@example.test"), false), http.StatusSeeOther)
	if len(h.mail.messages) != before {
		t.Fatal("registration sent an unnecessary verification email")
	}
	u, err := h.app.Accounts.GetByEmail(t.Context(), "writer@example.test")
	if err != nil || u.Invited || u.VerificationRequired || u.Verified || u.Role != "poster" {
		t.Fatalf("registration without verification: %+v %v", u, err)
	}
	requireStatus(t, h.request("POST", "/login", credentials(u.Email), false), http.StatusSeeOther)
	requireStatus(t, h.request("GET", "/post/create", nil, false), http.StatusOK)
	requireStatus(t, h.request("GET", link, nil, false), http.StatusBadRequest)
}

func TestPosterContentPermissions(t *testing.T) {
	h := usersHarness(t)
	adminCookies := h.cookies
	imageID, err := h.blog.images.Put(t.Context(), testImageUpload("image/png", testPNG(t)))
	if err != nil {
		t.Fatal(err)
	}
	adminPost, err := h.blog.persistPost(t.Context(), Post{Title: "Admin private", Slug: "admin-private", Markdown: "secret", Draft: true, PreviewImage: imageID})
	if err != nil {
		t.Fatal(err)
	}
	link := inviteUserForTest(t, h, "poster@example.test", "poster")
	h.cookies = make(map[string]*http.Cookie)
	h.request("GET", link, nil, false)
	requireStatus(t, h.request("POST", "/register", credentials("poster@example.test"), false), http.StatusSeeOther)
	requireStatus(t, h.request("POST", "/login", credentials("poster@example.test"), false), http.StatusSeeOther)
	poster, _ := h.app.Accounts.GetByEmail(t.Context(), "poster@example.test")
	requireStatus(t, h.request("GET", "/images/"+imageID, nil, false), http.StatusNotFound)
	requireStatus(t, h.request("GET", "/post/s/admin-private", nil, false), http.StatusNotFound)
	for _, path := range []string{"/users", "/pages", "/profile", "/media", "/history/post/" + adminPost.ID, "/post/update/" + adminPost.ID, "/post/delete/" + adminPost.ID} {
		for _, hx := range []bool{true, false} {
			requireStatus(t, h.request("GET", path, nil, hx), http.StatusForbidden)
		}
	}
	for _, path := range []string{"/users", "/pages", "/profile", "/media/upload", "/post/" + adminPost.ID, "/post/delete/" + adminPost.ID, "/history/post/" + adminPost.ID + "/restore"} {
		requireStatus(t, h.request("POST", path, url.Values{}, false), http.StatusForbidden)
	}
	listing := h.request("GET", "/post/manage", nil, false)
	requireStatus(t, listing, http.StatusOK)
	if strings.Contains(listing.Body.String(), adminPost.Title) || strings.Contains(listing.Body.String(), `href="/users"`) {
		t.Fatal("poster saw another user's management content")
	}
	fields := url.Values{"title": {"Own post"}, "slug": {"own-post"}, "content": {"My post"}, "published": {"on"}, "author_id": {"forged"}}
	fields.Set("previewImage", imageID)
	requireStatus(t, h.request("POST", "/post", fields, false), http.StatusUnprocessableEntity)
	fields.Del("previewImage")
	fields.Set("content", "![External](https://elsewhere.example/images/"+imageID+")")
	requireStatus(t, h.request("POST", "/post", fields, false), http.StatusSeeOther)
	posts, err := h.blog.posts.ListMetadata(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var own Post
	for _, p := range posts {
		if p.Slug == "own-post" {
			own = p
		}
	}
	if own.ID == "" || own.AuthorID != poster.ID || own.Draft {
		t.Fatalf("post owner: %+v", own)
	}
	fields.Set("title", "Edited own post")
	requireStatus(t, h.submitEditor(t, "/post/"+own.ID, fields, false), http.StatusSeeOther)
	posterCookies := h.cookies
	h.cookies = adminCookies
	fields.Set("title", "Admin edit")
	requireStatus(t, h.submitEditor(t, "/post/"+own.ID, fields, false), http.StatusSeeOther)
	saved, _ := h.blog.posts.GetByID(t.Context(), own.ID, true)
	if saved.AuthorID != poster.ID {
		t.Fatal("admin edit changed authorship")
	}
	h.cookies = posterCookies
	requireStatus(t, h.request("GET", "/history/post/"+own.ID, nil, false), http.StatusOK)
	requireStatus(t, h.request("POST", "/post/delete/"+own.ID, url.Values{}, false), http.StatusSeeOther)
	h.cookies = adminCookies
	poster.Role = ""
	admin, _ := h.app.Accounts.GetByEmail(t.Context(), "trentnix@gmail.com")
	if err := h.blog.administration.ChangeUserRole(t.Context(), admin.ID, "admin", poster); err != nil {
		t.Fatal(err)
	}
	h.cookies = posterCookies
	requireStatus(t, h.request("GET", "/post/manage", nil, false), http.StatusForbidden)
	requireStatus(t, h.request("POST", "/login", credentials(poster.Email), false), http.StatusForbidden)
}

func TestInvitedExternalRegistration(t *testing.T) {
	cfg := harnessConfig(t, 10)
	cfg.Auth.Services["test-external"] = map[string]string{"enabled": "true", "email": "external@example.test", "signInOnRegistration": "true"}
	h := startHarness(t, cfg)
	if err := h.blog.access.setup.finish(t.Context()); err != nil {
		t.Fatal(err)
	}
	h.register(t)
	h.verify(t)
	h.login(t)
	requireStatus(t, h.request("GET", "/users", nil, false), http.StatusOK)
	inviteUserForTest(t, h, "external@example.test", "poster")
	h.cookies = make(map[string]*http.Cookie)
	// Verified provider email supplies proof without an invitation cookie.
	requireStatus(t, completeThirdProviderFlow(t, h, "/register"), http.StatusSeeOther)
	u, err := h.app.Accounts.GetByEmail(t.Context(), "external@example.test")
	if err != nil || u.Invited || !u.Verified || u.Role != "poster" {
		t.Fatalf("external invitation: %+v %v", u, err)
	}
	requireStatus(t, h.request("GET", "/post/create", nil, false), http.StatusOK)
}

func TestInvitationDeliveryFailureAndResend(t *testing.T) {
	h := usersHarness(t)
	h.mail.err = errors.New("mail unavailable")
	w := h.request("POST", "/users", url.Values{"email": {"writer@example.test"}, "role": {"poster"}}, false)
	requireStatus(t, w, http.StatusBadGateway)
	if !strings.Contains(w.Body.String(), "could not be sent") || !strings.Contains(w.Body.String(), "Resend invitation") {
		t.Fatal("failed delivery lacks recovery", w.Body.String())
	}
	u, err := h.app.Accounts.GetByEmail(t.Context(), "writer@example.test")
	if err != nil || !u.Invited {
		t.Fatalf("pending invitation missing: %+v %v", u, err)
	}
	h.mail.err = nil
	values := url.Values{"version": {strconv.FormatInt(u.SessionVersion, 10)}}
	requireStatus(t, h.request("POST", "/users/"+u.ID+"/invite", values, false), http.StatusSeeOther)
	requireStatus(t, h.request("POST", "/users/"+u.ID+"/invite", values, false), http.StatusConflict)
	u, _ = h.app.Accounts.GetByID(t.Context(), u.ID)
	values.Set("version", strconv.FormatInt(u.SessionVersion, 10))
	requireStatus(t, h.request("POST", "/users/"+u.ID+"/remove", values, false), http.StatusSeeOther)
	h.cookies = make(map[string]*http.Cookie)
	requireStatus(t, h.request("POST", "/register", credentials(u.Email), false), http.StatusSeeOther)
	u, _ = h.app.Accounts.GetByID(t.Context(), u.ID)
	if u.Role != "" || u.Password != "" {
		t.Fatal("canceled invitation registered")
	}
}

func TestLastAdminControlsAndSubmissions(t *testing.T) {
	h := usersHarness(t)
	admin, err := h.app.Accounts.GetByEmail(t.Context(), "trentnix@gmail.com")
	if err != nil {
		t.Fatal(err)
	}
	path := "/users/" + admin.ID
	posterInput := regexp.MustCompile(`<input[^>]*id="user-role-poster"[^>]*>`)
	saveButton := regexp.MustCompile(`<button[^>]*type="submit"[^>]*>Save changes</button>`)
	for _, state := range []string{"only user", "pending admin", "second active admin"} {
		if state == "pending admin" {
			inviteUserForTest(t, h, "another@example.test", "admin")
		}
		if state == "second active admin" {
			u, err := h.app.Accounts.GetByEmail(t.Context(), "another@example.test")
			if err != nil {
				t.Fatal(err)
			}
			u.Invited, u.Verified, u.RegistrationAuthType, u.Password = false, true, "email", "hash"
			if err := h.app.Accounts.Update(t.Context(), u); err != nil {
				t.Fatal(err)
			}
		}
		for _, hx := range []bool{false, true} {
			listing := h.request("GET", "/users", nil, hx)
			requireStatus(t, listing, http.StatusOK)
			if strings.Contains(listing.Body.String(), `href="`+path+`"`) != (state != "only user") {
				t.Fatalf("incorrect Edit access visibility for %s", state)
			}
			response := h.request("GET", path, nil, hx)
			requireStatus(t, response, http.StatusOK)
			input := posterInput.FindString(response.Body.String())
			if input == "" || strings.Contains(input, " disabled") != (state != "second active admin") {
				t.Fatalf("incorrect Poster control for %s: %s", state, input)
			}
			button := saveButton.FindString(response.Body.String())
			if button == "" || strings.Contains(button, " disabled") != (state != "second active admin") {
				t.Fatalf("incorrect Save changes control for %s: %s", state, button)
			}
			if strings.Contains(response.Body.String(), path+"/remove") {
				t.Fatal("admin can see self-removal control")
			}
			if state != "second active admin" {
				response = h.request("POST", path, url.Values{"role": {"poster"}, "version": {strconv.FormatInt(admin.SessionVersion, 10)}}, hx)
				requireStatus(t, response, http.StatusConflict)
				if !strings.Contains(posterInput.FindString(response.Body.String()), " disabled") || !strings.Contains(response.Body.String(), "last-admin-help") {
					t.Fatal("rejected demotion did not restore protected controls")
				}
			}
			requireStatus(t, h.request("POST", path+"/remove", url.Values{"version": {strconv.FormatInt(admin.SessionVersion, 10)}}, hx), http.StatusForbidden)
		}
	}
	stored, err := h.app.Accounts.GetByID(t.Context(), admin.ID)
	if err != nil || stored.Role != "admin" || stored.SessionVersion != admin.SessionVersion {
		t.Fatal("rejected changes modified the admin", err)
	}
}

func TestCanceledInvitationCannotBeEdited(t *testing.T) {
	h := usersHarness(t)
	inviteUserForTest(t, h, "canceled@example.test", "poster")
	u, err := h.app.Accounts.GetByEmail(t.Context(), "canceled@example.test")
	if err != nil {
		t.Fatal(err)
	}
	path := "/users/" + u.ID
	values := url.Values{"version": {strconv.FormatInt(u.SessionVersion, 10)}, "role": {"poster"}}
	requireStatus(t, h.request("POST", path+"/remove", values, false), http.StatusSeeOther)
	u, err = h.app.Accounts.GetByID(t.Context(), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, htmx := range []bool{false, true} {
		requireStatus(t, h.request("GET", path, nil, htmx), http.StatusNotFound)
		// Reject both a stale form and a request with the latest account version.
		for _, version := range []int64{u.SessionVersion - 1, u.SessionVersion} {
			values.Set("version", strconv.FormatInt(version, 10))
			requireStatus(t, h.request("POST", path, values, htmx), http.StatusNotFound)
		}
	}
	link := inviteUserForTest(t, h, u.Email, "poster")
	h.cookies = make(map[string]*http.Cookie)
	requireStatus(t, h.request("GET", link, nil, false), http.StatusSeeOther)
	requireStatus(t, h.request("POST", "/register", credentials(u.Email), false), http.StatusSeeOther)
	u, err = h.app.Accounts.GetByID(t.Context(), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if u.Invited || !u.Verified || u.Role != "poster" || u.RegistrationAuthType != "email" {
		t.Fatal("reinvited account could not complete registration")
	}
}

var _ user.AdministrationRepository = (*unavailableAdminAccounts)(nil)

func registeredPoster(t *testing.T, h *harness) *user.User {
	t.Helper()
	link := inviteUserForTest(t, h, "review-poster@example.test", "poster")
	h.cookies = make(map[string]*http.Cookie)
	requireStatus(t, h.request("GET", link, nil, false), http.StatusSeeOther)
	requireStatus(t, h.request("POST", "/register", credentials("review-poster@example.test"), false), http.StatusSeeOther)
	requireStatus(t, h.request("POST", "/login", credentials("review-poster@example.test"), false), http.StatusSeeOther)
	u, err := h.app.Accounts.GetByEmail(t.Context(), "review-poster@example.test")
	if err != nil {
		t.Fatal(err)
	}
	return u
}

type revokingPostRepository struct {
	repository
	revoke func()
}

func (p revokingPostRepository) ListMetadata(ctx context.Context) ([]Post, error) {
	posts, err := p.repository.ListMetadata(ctx)
	p.revoke()
	return posts, err
}

func TestRevocationDuringPostList(t *testing.T) {
	h := usersHarness(t)
	admin, err := h.app.Accounts.GetByEmail(t.Context(), "trentnix@gmail.com")
	if err != nil {
		t.Fatal(err)
	}
	_, err = h.blog.persistPost(t.Context(), Post{Title: "Private admin draft review marker", Slug: "private-admin-review", Markdown: "Private", Draft: true, AuthorID: admin.ID})
	if err != nil {
		t.Fatal(err)
	}
	poster := registeredPoster(t, h)
	h.blog.posts = revokingPostRepository{h.blog.posts, func() {
		poster.Role = ""
		if err := h.blog.administration.ChangeUserRole(t.Context(), admin.ID, "admin", poster); err != nil {
			t.Fatal(err)
		}
	}}
	w := h.request("GET", "/post/manage", nil, false)
	requireStatus(t, w, http.StatusOK)
	if strings.Contains(w.Body.String(), "Private admin draft review marker") {
		t.Fatalf("revoked Poster received another author's private draft: HTTP %d", w.Code)
	}
	requireStatus(t, h.request("GET", "/post/manage", nil, false), http.StatusForbidden)
}

func TestPostManagementDeniesMissingRole(t *testing.T) {
	h := usersHarness(t)
	for _, permission := range []string{"", "unknown"} {
		account := &user.User{ID: "no-access", Email: "no-access@example.test", Role: permission}
		r := user.AddUserToRequestContext(httptest.NewRequest("GET", "/post/manage", nil), account)
		if _, err := h.blog.postManagement(r.Context(), ""); !errors.Is(err, errAccessDenied) {
			t.Fatalf("role %q: %v", permission, err)
		}
	}
	if _, err := h.blog.postManagement(t.Context(), ""); !errors.Is(err, errAccessDenied) {
		t.Fatalf("anonymous list: %v", err)
	}
}

func TestRoleUsesAuthenticatedRequestAccount(t *testing.T) {
	// No repository is supplied. An established request must not load the account again.
	access := &Access{}
	account := &user.User{ID: "poster", Role: "poster", Verified: true}
	for range 3 {
		permission, err := access.Role(t.Context(), account)
		if err != nil || permission != rolePoster {
			t.Fatalf("request role: %q %v", permission, err)
		}
	}
	account.Invited = true
	if permission, err := access.Role(t.Context(), account); err != nil || permission != "" {
		t.Fatalf("pending invitation granted access: %q %v", permission, err)
	}
}

func TestUnrelatedInvitationCookie(t *testing.T) {
	h := usersHarness(t)
	linkA := inviteUserForTest(t, h, "invite-a@example.test", "poster")
	inviteUserForTest(t, h, "invite-b@example.test", "poster")
	h.cookies = make(map[string]*http.Cookie)
	requireStatus(t, h.request("POST", "/register", credentials("invite-b@example.test"), false), http.StatusSeeOther)
	requireStatus(t, h.request("GET", linkA, nil, false), http.StatusSeeOther)
	w := h.request("POST", "/login", credentials("invite-b@example.test"), false)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("valid credentials blocked by another recipient's invitation cookie: HTTP %d", w.Code)
	}
	if w.Header().Get("Location") != "/auth/request/verify" {
		t.Fatal("unverified invitee bypassed verification")
	}
	u, err := h.app.Accounts.GetByEmail(t.Context(), "invite-b@example.test")
	if err != nil || u.Verified || !u.Invited {
		t.Fatalf("unrelated invitation verified account: %+v %v", u, err)
	}
	if _, exists := h.cookies[invitationCookie]; exists {
		t.Fatal("login retained the unrelated invitation cookie")
	}
}

func TestDismissInvitation(t *testing.T) {
	h := usersHarness(t)
	link := inviteUserForTest(t, h, "invited@example.test", "poster")
	// An invitation for someone else must not change the signed-in admin's browser state.
	requireStatus(t, h.request("GET", link, nil, false), http.StatusForbidden)
	if _, exists := h.cookies[invitationCookie]; exists {
		t.Fatal("another account's invitation set a cookie")
	}
	h.cookies = make(map[string]*http.Cookie)
	requireStatus(t, h.request("GET", link, nil, false), http.StatusSeeOther)
	for _, path := range []string{"/register", "/login"} {
		page := h.request("GET", path, nil, false)
		requireStatus(t, page, http.StatusOK)
		if !strings.Contains(page.Body.String(), `action="/invite/dismiss"`) {
			t.Fatalf("%s has no invitation dismissal", path)
		}
	}
	requireStatus(t, h.request("POST", "/invite/dismiss", nil, false), http.StatusSeeOther)
	if _, exists := h.cookies[invitationCookie]; exists {
		t.Fatal("dismissal retained invitation cookie")
	}
	u, err := h.app.Accounts.GetByEmail(t.Context(), "invited@example.test")
	if err != nil || !u.Invited || u.Verified {
		t.Fatalf("dismissal changed invitation: %+v %v", u, err)
	}
	requireStatus(t, h.request("GET", link, nil, false), http.StatusSeeOther)
}

type failedInvitationRepository struct{ user.AccountRepository }

func (failedInvitationRepository) GetToken(context.Context, string, string) (user.TokenMetadata, error) {
	return user.TokenMetadata{}, errors.New("private invitation storage failure")
}

func TestInvitationLookupFailurePreservesLink(t *testing.T) {
	h := usersHarness(t)
	link := inviteUserForTest(t, h, "invited@example.test", "poster")
	h.cookies = make(map[string]*http.Cookie)
	requireStatus(t, h.request("GET", link, nil, false), http.StatusSeeOther)
	h.blog.accounts = failedInvitationRepository{h.app.Accounts}
	w := h.request("GET", "/register", nil, false)
	requireStatus(t, w, http.StatusServiceUnavailable)
	if strings.Contains(w.Body.String(), "private invitation storage failure") {
		t.Fatal("invitation error exposed storage details")
	}
	if _, exists := h.cookies[invitationCookie]; !exists {
		t.Fatal("temporary storage failure discarded invitation")
	}
	h.blog.accounts = h.app.Accounts
	requireStatus(t, h.request("GET", "/register", nil, false), http.StatusOK)
}

type failedInvitationAcceptance struct {
	user.AccountRepository
	user.InvitationRepository
}

func (failedInvitationAcceptance) AcceptInvitation(context.Context, *user.User, *user.TokenMetadata) error {
	return errors.New("private invitation storage failure")
}

func TestInvitationFailuresAreRetryable(t *testing.T) {
	h := usersHarness(t)
	link := inviteUserForTest(t, h, "retry@example.test", "poster")
	h.cookies = make(map[string]*http.Cookie)
	for _, hx := range []bool{false, true} {
		h.blog.accounts = failedInvitationRepository{h.app.Accounts}
		w := h.request("GET", link, nil, hx)
		requireStatus(t, w, http.StatusServiceUnavailable)
		if strings.Contains(w.Body.String(), "invalid or has expired") || strings.Contains(w.Body.String(), "private invitation storage failure") {
			t.Fatal("lookup failure misreported or exposed storage details")
		}
	}
	h.blog.accounts = h.app.Accounts
	// Register without the link so acceptance still needs its email proof.
	requireStatus(t, h.request("POST", "/register", credentials("retry@example.test"), false), http.StatusSeeOther)
	requireStatus(t, h.request("POST", "/login", credentials("retry@example.test"), false), http.StatusSeeOther)
	requireStatus(t, h.request("GET", link, nil, false), http.StatusSeeOther)
	account, err := h.app.Accounts.GetByEmail(t.Context(), "retry@example.test")
	if err != nil {
		t.Fatal(err)
	}
	for _, phase := range []string{"lookup", "accept"} {
		for _, hx := range []bool{false, true} {
			if phase == "lookup" {
				h.blog.accounts = failedInvitationRepository{h.app.Accounts}
			} else {
				h.blog.accounts = failedInvitationAcceptance{h.app.Accounts, h.app.Accounts.(user.InvitationRepository)}
			}
			r := httptest.NewRequest("POST", "https://blog.example/invite/accept", nil)
			r = user.AddUserToRequestContext(r, account)
			r = r.WithContext(user.WithInvitation(r.Context(), h.cookies[invitationCookie].Value))
			if hx {
				r.Header.Set("HX-Request", "true")
			}
			w := httptest.NewRecorder()
			h.blog.acceptInvitation(w, r)
			requireStatus(t, w, http.StatusServiceUnavailable)
			if strings.Contains(w.Body.String(), "invalid or has expired") || strings.Contains(w.Body.String(), "private invitation storage failure") {
				t.Fatal("acceptance failure misreported or exposed storage details")
			}
			if len(w.Result().Cookies()) != 0 {
				t.Fatal("failed acceptance changed browser credentials")
			}
		}
	}
	h.blog.accounts = h.app.Accounts
	requireStatus(t, h.request("POST", "/invite/accept", nil, false), http.StatusSeeOther)
	stored, err := h.app.Accounts.GetByID(t.Context(), account.ID)
	if err != nil || stored.Invited || !stored.Verified {
		t.Fatalf("retry did not accept invitation: %+v %v", stored, err)
	}
}
