package google

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/trentnix/hyperserver/auth"
	"github.com/trentnix/hyperserver/pkg/services/logger"
	"github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/user"
	"golang.org/x/oauth2"
)

// flow is short-lived, browser-bound server-side state. A callback consumes it
// through atomic session rotation before contacting Google or changing accounts.
type flow struct {
	State, Nonce, Verifier, Mode string
	AccountID                    string
	SessionVersion               int64
	Expires                      time.Time
}

// Login starts sign-in. The callback returns the account to the shared manager.
func (s *Service) Login(w http.ResponseWriter, r *http.Request) auth.LoginResult {
	s.start(w, r, "login")
	return auth.LoginResult{}
}

// Register starts registration only when application configuration permits it.
func (s *Service) Register(w http.ResponseWriter, r *http.Request) auth.RegistrationResult {
	if !s.registration {
		s.fail(w, r, "Registration is not available.", nil, http.StatusForbidden)
		return auth.RegistrationResult{}
	}
	s.start(w, r, "register")
	return auth.RegistrationResult{}
}

func (s *Service) start(w http.ResponseWriter, r *http.Request, mode string) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	f := flow{State: oauth2.GenerateVerifier(), Nonce: oauth2.GenerateVerifier(), Verifier: oauth2.GenerateVerifier(), Mode: mode, Expires: time.Now().Add(10 * time.Minute)}
	if mode == "link" {
		account := user.GetUserFromContext(r.Context())
		if account == nil || account.NeedsVerification() {
			s.fail(w, r, "Log in and verify your account before connecting Google.", nil, http.StatusForbidden)
			return
		}
		f.AccountID, f.SessionVersion = account.ID, account.SessionVersion
	}
	state, err := session.Get(r, flowSession)
	if errors.Is(err, session.ErrInvalidToken) || errors.Is(err, session.ErrSessionInvalid) {
		// A malformed flow cookie must not prevent starting a new login.
		state, err = session.New(r, flowSession)
	}
	if err == nil {
		err = state.Rotate(w, r, map[string]any{"flow": f})
	}
	if err != nil {
		s.fail(w, r, "Google sign-in could not start. Please try again.", err, http.StatusServiceUnavailable)
		return
	}
	logger.Audit(r.Context(), "google_login", "started")
	// This must be a browser navigation, never an HTMX request to Google's origin.
	location := s.oauth.AuthCodeURL(f.State, oidc.Nonce(f.Nonce), oauth2.S256ChallengeOption(f.Verifier), oauth2.SetAuthURLParam("prompt", "select_account"))
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", location)
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, location, http.StatusSeeOther)
}

// Callback validates a one-use flow and Google ID token before resolving accounts.
func (s *Service) Callback(w http.ResponseWriter, r *http.Request) *user.User {
	w.Header().Set("Referrer-Policy", "no-referrer")
	state, err := session.Get(r, flowSession)
	if err != nil && !errors.Is(err, session.ErrInvalidToken) && !errors.Is(err, session.ErrSessionInvalid) {
		s.fail(w, r, "Google sign-in is temporarily unavailable. Please try again.", err, http.StatusServiceUnavailable)
		return nil
	}
	var f flow
	if err == nil {
		err = state.DecodeValue("flow", &f)
	}
	values := r.URL.Query()
	if err != nil || state.IsNew || f.State == "" || !time.Now().Before(f.Expires) || len(values["state"]) != 1 || subtle.ConstantTimeCompare([]byte(f.State), []byte(values.Get("state"))) != 1 {
		s.fail(w, r, "Google sign-in expired or could not be verified. Please start again.", nil, http.StatusBadRequest)
		return nil
	}
	if err := state.Rotate(w, r, map[string]any{}); err != nil {
		if errors.Is(err, session.ErrSessionInvalid) {
			s.fail(w, r, "Google sign-in has already been used. Please start again.", nil, http.StatusBadRequest)
		} else {
			s.fail(w, r, "Google sign-in is temporarily unavailable. Please try again.", err, http.StatusServiceUnavailable)
		}
		return nil
	}
	if f.Mode != "link" && user.GetUserFromContext(r.Context()) != nil {
		s.fail(w, r, "You're already logged in.", nil, http.StatusForbidden)
		return nil
	}
	if values.Get("error") != "" {
		s.fail(w, r, "Google sign-in was canceled. You can try again from the login page.", nil, http.StatusBadRequest)
		return nil
	}
	if len(values["code"]) != 1 || values.Get("code") == "" {
		s.fail(w, r, "Google did not return a sign-in code. Please start again.", nil, http.StatusBadRequest)
		return nil
	}
	ctx := oidc.ClientContext(r.Context(), s.client)
	tokens, err := s.oauth.Exchange(ctx, values.Get("code"), oauth2.VerifierOption(f.Verifier))
	if err != nil {
		// OAuth errors may contain codes, credentials, or response bodies. Do not log them.
		s.fail(w, r, "Google sign-in could not be completed. Please try again.", nil, http.StatusBadGateway)
		return nil
	}
	raw, _ := tokens.Extra("id_token").(string)
	token, err := s.verifier.Verify(ctx, raw)
	if err != nil || token.Subject == "" || subtle.ConstantTimeCompare([]byte(token.Nonce), []byte(f.Nonce)) != 1 {
		s.fail(w, r, "Google could not verify your identity. Please start again.", nil, http.StatusUnauthorized)
		return nil
	}
	var claims struct {
		Email    string `json:"email"`
		Verified bool   `json:"email_verified"`
		Domain   string `json:"hd"`
	}
	if token.Claims(&claims) != nil || !claims.Verified || user.EmailKey(claims.Email) == "" {
		s.fail(w, r, "Use a Google account with a verified email address.", nil, http.StatusForbidden)
		return nil
	}
	if !s.Policy.Admit(w, r, claims.Email, s.content) {
		return nil
	}
	// For third-party email addresses, email_verified alone does not establish
	// current ownership. Those users must register through email and explicitly link.
	verifiedEmail := ""
	if strings.HasSuffix(strings.ToLower(claims.Email), "@gmail.com") || claims.Domain != "" {
		verifiedEmail = claims.Email
	}
	accounts := auth.ExternalAccounts{
		Identities: s.identities, Policy: s.Policy, Content: s.content,
		Provider: AuthType, RegistrationEnabled: s.registration,
	}
	account, _ := accounts.Resolve(w, r, auth.ExternalAccountRequest{
		Identity: user.ExternalIdentity{Issuer: issuer, Subject: token.Subject},
		Mode:     f.Mode, AccountID: f.AccountID, SessionVersion: f.SessionVersion, VerifiedEmail: verifiedEmail,
	})
	// Google has authenticated newly registered accounts too. Let the manager
	// establish the session and apply the application's normal login behavior.
	return account
}

func (s *Service) fail(w http.ResponseWriter, r *http.Request, message string, err error, status int) {
	logger.Audit(r.Context(), "google_login", "rejected")
	s.content.HandleError(w, r, message, err, status)
}
