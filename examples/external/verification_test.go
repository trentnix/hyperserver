package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"html"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/trentnix/hyperserver/pkg/services/messaging"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

type capturedMail struct {
	mu       sync.Mutex
	messages []messaging.MailMessage
	err      error
}

func (m *capturedMail) Send(ctx context.Context, message messaging.MailMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if m.err != nil {
		return m.err
	}
	m.messages = append(m.messages, message)
	return nil
}

func (m *capturedMail) snapshot() []messaging.MailMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.messages)
}

func (m *capturedMail) fail(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.err = err
}

type verificationHarness struct {
	app     *application
	handler http.Handler
	jar     http.CookieJar
	hx      bool
}

func newVerificationHarness(t *testing.T, s settings, hx bool) *verificationHarness {
	t.Helper()
	app, err := newApplication(t.Context(), s)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := app.Shutdown(); err != nil {
			t.Error(err)
		}
	})
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &verificationHarness{app: app, handler: newHTTPServer(app).Handler, jar: jar, hx: hx}
}

func (h *verificationHarness) request(t *testing.T, method, path string, values url.Values) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, "http://example.test"+path, strings.NewReader(values.Encode()))
	if method == "POST" {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Origin", "http://example.test")
	}
	if h.hx {
		r.Header.Set("HX-Request", "true")
	}
	for _, cookie := range h.jar.Cookies(r.URL) {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	h.handler.ServeHTTP(w, r)
	h.jar.SetCookies(r.URL, w.Result().Cookies())
	return w
}

func (h *verificationHarness) redirect(t *testing.T, w *httptest.ResponseRecorder, destination string) {
	t.Helper()
	status, header := 303, "Location"
	if h.hx {
		status, header = 200, "HX-Redirect"
	}
	if w.Code != status || w.Header().Get(header) != destination || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("redirect = %d %v, want %s; %s", w.Code, w.Header(), destination, w.Body)
	}
}

func registrationValues(email string) url.Values {
	return url.Values{"email": {email}, "password": {testPassword}, "passwordMatch": {testPassword}}
}

func verificationLink(t *testing.T, body string) *url.URL {
	t.Helper()
	for _, match := range regexp.MustCompile(`href="([^"]+)"`).FindAllStringSubmatch(body, -1) {
		link, err := url.Parse(html.UnescapeString(match[1]))
		if err == nil && link.Path == "/auth/verify" && link.Query().Get("token") != "" {
			return link
		}
	}
	t.Fatal("email contains no verification link")
	return nil
}

func verificationSettings(t *testing.T, sender messaging.MailSender) settings {
	s := testSettings(t, "files", filepath.Join(t.TempDir(), "notes"))
	s.registration, s.publicOrigin, s.mailSender = true, "http://127.0.0.1:8083", sender
	return s
}

func TestRegistrationAndVerification(t *testing.T) {
	for _, hx := range []bool{false, true} {
		name := "navigation"
		if hx {
			name = "HTMX"
		}
		t.Run(name, func(t *testing.T) {
			mail := &capturedMail{}
			h := newVerificationHarness(t, verificationSettings(t, mail), hx)
			w := h.request(t, "GET", "/auth/register/email", nil)
			if w.Code != 200 || !strings.Contains(w.Body.String(), `action="/auth/register/email"`) || strings.Contains(w.Body.String(), "<!doctype html>") == hx {
				t.Fatal("registration form", w.Code, w.Body)
			}
			crossOrigin := httptest.NewRequest("POST", "http://example.test/auth/register/email", strings.NewReader(registrationValues(testEmail).Encode()))
			crossOrigin.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			crossOrigin.Header.Set("Origin", "https://attacker.test")
			blocked := httptest.NewRecorder()
			h.handler.ServeHTTP(blocked, crossOrigin)
			if blocked.Code != 403 || len(mail.snapshot()) != 0 {
				t.Fatal("cross-origin registration accepted")
			}
			w = h.request(t, "POST", "/auth/request/verify", nil)
			if w.Code != 401 || len(mail.snapshot()) != 0 {
				t.Fatal("anonymous resend accepted")
			}
			invalid := registrationValues("invalid")
			w = h.request(t, "POST", "/auth/register/email", invalid)
			if w.Code != 200 || !strings.Contains(w.Body.String(), "aria-invalid") || len(mail.snapshot()) != 0 {
				t.Fatal("invalid registration accepted", w.Code, w.Body)
			}
			w = h.request(t, "POST", "/auth/register/email", registrationValues(testEmail))
			h.redirect(t, w, "/auth/login/email")
			messages := mail.snapshot()
			if len(messages) != 1 || messages[0].To != testEmail {
				t.Fatal("wrong registration delivery", len(messages))
			}
			link := verificationLink(t, messages[0].Body)
			if link.Scheme != "http" || link.Host != "127.0.0.1:8083" || strings.Contains(w.Body.String(), link.Query().Get("token")) {
				t.Fatal("request host influenced link or response exposed token")
			}
			account, err := h.app.accounts.GetByEmail(t.Context(), testEmail)
			if err != nil || !account.NeedsVerification() {
				t.Fatal("new account does not require verification", err)
			}
			w = h.request(t, "POST", "/auth/register/email", registrationValues(strings.ToUpper(testEmail)))
			if w.Header().Get("Location") != "" || w.Header().Get("HX-Redirect") != "" || len(mail.snapshot()) != 1 {
				t.Fatal("duplicate registration accepted")
			}

			w = h.request(t, "POST", "/auth/login/email", registrationValues(testEmail))
			h.redirect(t, w, "/auth/request/verify")
			w = h.request(t, "GET", "/notes", nil)
			if w.Code != 200 || !strings.Contains(w.Body.String(), "Verify your email") || strings.Contains(w.Body.String(), `action="/notes"`) {
				t.Fatal("unverified account offered a note form", w.Code, w.Body)
			}
			if w := h.request(t, "POST", "/notes", url.Values{"text": {"must-not-save"}}); w.Code != 403 {
				t.Fatal("unverified write accepted", w.Code)
			}
			w = h.request(t, "GET", "/auth/request/verify", nil)
			if w.Code != 200 || !strings.Contains(w.Body.String(), "Send verification email") || len(mail.snapshot()) != 1 {
				t.Fatal("resend page sent mail or lost its controls")
			}
			w = h.request(t, "POST", "/auth/request/verify", url.Values{"email": {"attacker@example.com"}})
			if w.Code != 200 || !strings.Contains(w.Body.String(), "Verification email sent") {
				t.Fatal("resend failed", w.Code, w.Body)
			}
			messages = mail.snapshot()
			if len(messages) != 2 || messages[1].To != testEmail {
				t.Fatal("resend did not use the stored address")
			}
			resent := verificationLink(t, messages[1].Body)
			if resent.String() == link.String() {
				t.Fatal("resend reused the first token")
			}
			w = h.request(t, "GET", resent.RequestURI(), nil)
			if w.Code != 200 || !strings.Contains(w.Body.String(), `action="/auth/verify"`) || w.Header().Get("Referrer-Policy") != "no-referrer" || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("missing safe confirmation form", w.Code, w.Body)
			}
			account, err = h.app.accounts.GetByEmail(t.Context(), testEmail)
			if err != nil || !account.NeedsVerification() {
				t.Fatal("GET consumed the token", err)
			}
			w = h.request(t, "POST", resent.RequestURI(), nil)
			if w.Code != 400 {
				t.Fatal("query-only POST accepted", w.Code)
			}
			w = h.request(t, "POST", "/auth/verify", resent.Query())
			h.redirect(t, w, "/notes")
			w = h.request(t, "POST", "/notes", url.Values{"text": {"verified-note"}})
			h.redirect(t, w, "/notes")
			w = h.request(t, "GET", "/notes", nil)
			if !strings.Contains(w.Body.String(), "verified-note") || strings.Contains(w.Body.String(), "must-not-save") {
				t.Fatal("wrong persisted notes", w.Body)
			}
			w = h.request(t, "POST", "/auth/verify", resent.Query())
			if w.Code != 400 || !strings.Contains(w.Body.String(), "Verification failed") || strings.Contains(w.Body.String(), resent.Query().Get("token")) {
				t.Fatal("replay accepted or exposed token", w.Code, w.Body)
			}
			w = h.request(t, "POST", "/auth/request/verify", nil)
			if w.Code != 200 || !strings.Contains(w.Body.String(), "No verification needed") || len(mail.snapshot()) != 2 {
				t.Fatal("verified account triggered more mail")
			}
		})
	}
}

func TestVerificationDeliveryFailure(t *testing.T) {
	for _, hx := range []bool{false, true} {
		mail := &capturedMail{err: errors.New("private-SMTP-detail")}
		h := newVerificationHarness(t, verificationSettings(t, mail), hx)
		w := h.request(t, "POST", "/auth/register/email", registrationValues(testEmail))
		if w.Code != 200 || !strings.Contains(w.Body.String(), "Your account was created") || !strings.Contains(w.Body.String(), `href="/auth/login/email"`) || strings.Contains(w.Body.String(), "private-SMTP-detail") || strings.Contains(w.Body.String(), `id="register-form"`) {
			t.Fatal("bad failed-delivery guidance", w.Code, w.Body)
		}
		if len(mail.snapshot()) != 0 {
			t.Fatal("failed delivery recorded success")
		}
		w = h.request(t, "POST", "/auth/login/email", registrationValues(testEmail))
		h.redirect(t, w, "/auth/request/verify")
		w = h.request(t, "POST", "/auth/request/verify", nil)
		if w.Code != 503 || !strings.Contains(w.Body.String(), "Send verification email") || strings.Contains(w.Body.String(), "private-SMTP-detail") {
			t.Fatal("bad resend failure", w.Code, w.Body)
		}
		mail.fail(nil)
		w = h.request(t, "POST", "/auth/request/verify", nil)
		if w.Code != 200 || len(mail.snapshot()) != 1 {
			t.Fatal("delivery did not recover", w.Code, w.Body)
		}
		link := verificationLink(t, mail.snapshot()[0].Body)
		mail.fail(errors.New("private-SMTP-detail"))
		if w := h.request(t, "POST", "/auth/request/verify", nil); w.Code != 503 {
			t.Fatal("second failure not reported")
		}
		w = h.request(t, "POST", "/auth/verify", link.Query())
		h.redirect(t, w, "/notes")
	}
}

func TestExpiredVerificationLink(t *testing.T) {
	mail := &capturedMail{}
	s := verificationSettings(t, mail)
	h := newVerificationHarness(t, s, false)
	h.request(t, "POST", "/auth/register/email", registrationValues(testEmail))
	u, err := h.app.accounts.GetByEmail(t.Context(), testEmail)
	if err != nil {
		t.Fatal(err)
	}
	expiry := time.Now().Add(-time.Hour)
	claims := user.VerificationClaims{Id: u.ID, Email: u.Email, Purpose: user.VerificationTokenPurpose,
		RegisteredClaims: jwt.RegisteredClaims{ID: "expired-test-token", ExpiresAt: jwt.NewNumericDate(expiry)}}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(s.authKey))
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256([]byte(token))
	if err := h.app.accounts.InsertToken(t.Context(), user.TokenMetadata{AccountID: u.ID, TokenHash: hex.EncodeToString(hash[:]), Purpose: user.VerificationTokenPurpose, ExpiresAt: expiry}); err != nil {
		t.Fatal(err)
	}
	for _, hx := range []bool{false, true} {
		h.hx = hx
		w := h.request(t, "POST", "/auth/verify", url.Values{"token": {token}})
		if w.Code != 400 || !strings.Contains(w.Body.String(), "Verification failed") || strings.Contains(w.Body.String(), token) {
			t.Fatal("expired token accepted or leaked", w.Code, w.Body)
		}
	}
	u, err = h.app.accounts.GetByEmail(t.Context(), testEmail)
	if err != nil || !u.NeedsVerification() {
		t.Fatal("expired link verified account", err)
	}
}

func TestRegistrationRequiresDeliveryAndOrigin(t *testing.T) {
	for _, tc := range []struct {
		name, origin, want string
		sender             messaging.MailSender
	}{
		{"missing origin", "", "public origin", &capturedMail{}},
		{"invalid origin", "https://example.test/path", "http.publicOrigin", &capturedMail{}},
		{"disabled delivery", "http://127.0.0.1:8083", "mail delivery is unavailable", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := verificationSettings(t, tc.sender)
			s.publicOrigin = tc.origin
			app, err := newApplication(t.Context(), s)
			if err == nil || app != nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("startup = %v, want %s", err, tc.want)
			}
		})
	}
}
