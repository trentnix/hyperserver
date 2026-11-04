package google

import (
	"bytes"
	"context"
	"errors"
	"image/png"
	"net/http"
	"strings"
	"testing"

	"github.com/trentnix/hyperserver/pkg/services/user"
)

func TestGoogleBrandControls(t *testing.T) {
	h := newHarness(t)
	w := h.request(http.MethodGet, "/auth/google/icon.png")
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "image/png" ||
		w.Header().Get("Cache-Control") != "public, max-age=86400" {
		t.Fatalf("icon: %d %v", w.Code, w.Header())
	}
	if _, err := png.Decode(bytes.NewReader(w.Body.Bytes())); err != nil {
		t.Fatalf("invalid brand image: %v", err)
	}
	for _, markup := range []string{string(h.service.GetLoginButton()), string(h.service.GetRegisterButton())} {
		if !strings.Contains(markup, `class="auth-google-button"`) && !strings.Contains(markup, `class="button auth-google-button"`) {
			t.Fatalf("missing branded control: %s", markup)
		}
		if !strings.Contains(markup, `src="/auth/google/icon.png" alt="" width="20" height="20"`) || !strings.Contains(markup, `hx-boost="false"`) {
			t.Fatalf("missing local decorative icon or browser navigation: %s", markup)
		}
	}
	for _, path := range []string{"/login", "/auth/register/google"} {
		w := h.request(http.MethodGet, path)
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `class="auth-google-button"`) ||
			!strings.Contains(w.Body.String(), `src="/auth/google/icon.png"`) {
			t.Fatalf("Google form at %s: %d %s", path, w.Code, w.Body)
		}
	}
}

func TestGoogleAccountConnectionStatus(t *testing.T) {
	for _, kind := range []string{"unconnected", "Google registration", "linked email"} {
		t.Run(kind, func(t *testing.T) {
			h := newHarness(t)
			var account *user.User
			if kind == "Google registration" {
				account = h.linkedAccount()
			} else {
				account = &user.User{Email: "local@example.com", RegistrationAuthType: "email"}
				if err := h.accounts.Create(t.Context(), account); err != nil {
					t.Fatal(err)
				}
				if kind == "linked email" {
					if err := h.accounts.LinkExternalIdentity(t.Context(), account, user.ExternalIdentity{Issuer: issuer, Subject: "subject"}); err != nil {
						t.Fatal(err)
					}
				}
			}
			h.signIn(account)
			before := account.SessionVersion
			for _, method := range []string{http.MethodGet, http.MethodPost} {
				w := h.request(method, "/auth/link/google")
				if kind == "unconnected" && method == http.MethodPost {
					if w.Code != http.StatusSeeOther {
						t.Fatalf("start link: %d %s", w.Code, w.Body)
					}
					continue
				}
				if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != "no-store" {
					t.Fatalf("connection page: %d %s", w.Code, w.Body)
				}
				wantConnected := kind != "unconnected"
				if strings.Contains(w.Body.String(), "Google is connected.") != wantConnected ||
					strings.Contains(w.Body.String(), "<form") == wantConnected ||
					!strings.Contains(w.Body.String(), "Back to Account") {
					t.Fatalf("incorrect connection controls: %s", w.Body)
				}
				if len(w.Result().Cookies()) != 0 {
					t.Fatal("displaying connection status changed sessions")
				}
			}
			got, err := h.accounts.GetByID(t.Context(), account.ID)
			if err != nil || got.SessionVersion != before || h.exchanges.Load() != 0 {
				t.Fatalf("connection controls changed account or contacted Google: %+v %v", got, err)
			}
		})
	}
}

type unavailableConnection struct {
	user.ExternalIdentityRepository
}

func (unavailableConnection) HasExternalIdentity(context.Context, string, string) (bool, error) {
	return false, errors.New("connection storage unavailable")
}

func TestGoogleAccountConnectionFailure(t *testing.T) {
	h := newHarness(t)
	h.signIn(h.linkedAccount())
	h.service.identities = unavailableConnection{h.accounts}
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		w := h.request(method, "/auth/link/google")
		if w.Code != http.StatusServiceUnavailable || len(w.Result().Cookies()) != 0 {
			t.Fatalf("connection failure: %d %s", w.Code, w.Body)
		}
	}
}

func TestGoogleLinkRateLimit(t *testing.T) {
	h := newHarness(t)
	account := &user.User{Email: "local@example.com"}
	if err := h.accounts.Create(t.Context(), account); err != nil {
		t.Fatal(err)
	}
	h.signIn(account)
	for i := range 21 {
		w := h.request(http.MethodPost, "/auth/link/google")
		want := http.StatusSeeOther
		if i == 20 {
			want = http.StatusTooManyRequests
			if len(w.Result().Cookies()) != 0 {
				t.Fatal("rate-limited request rotated a session")
			}
		}
		if w.Code != want {
			t.Fatalf("request %d: status %d, want %d", i+1, w.Code, want)
		}
	}
	if w := h.request(http.MethodGet, "/auth/link/google"); w.Code != http.StatusOK {
		t.Fatalf("mutation limit blocked the connection page: %d", w.Code)
	}
}
