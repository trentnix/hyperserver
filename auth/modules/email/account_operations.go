package auth

import (
	"bytes"
	"context"
	"errors"
	"html/template"
	"net/url"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/trentnix/hyperserver/auth/password"
	"github.com/trentnix/hyperserver/pkg/services/messaging"
	"github.com/trentnix/hyperserver/pkg/services/user"
)

var (
	errInvalidCredentials = errors.New("invalid credentials")
	errPasswordReused     = errors.New("password was already used")
)

// A fixed, valid hash supplies comparable work when no usable password exists.
// It is not an account credential. Keep its cost aligned with HashPassword.
const missingAccountHash = "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"

// Account operations use the provider's configured dependencies without HTTP,
// session, or notification state. Callers validate form fields before invoking
// these operations and translate their results into responses afterward.
func (a *EmailAuthService) authenticate(ctx context.Context, email, suppliedPassword string) (*user.User, error) {
	return a.authenticateWithPasswordCheck(ctx, email, suppliedPassword, bcrypt.CompareHashAndPassword)
}

func (a *EmailAuthService) authenticateWithPasswordCheck(ctx context.Context, email, suppliedPassword string, compare func([]byte, []byte) error) (*user.User, error) {
	// bcrypt comparisons ignore bytes beyond 72. Reject them before any account lookup.
	if len(suppliedPassword) > 72 {
		return nil, errInvalidCredentials
	}
	u, err := a.accounts.GetByEmail(ctx, email)
	if errors.Is(err, user.ErrUserNotFound) {
		u, err = nil, nil
	}
	if err != nil {
		return nil, err
	}

	hash := missingAccountHash
	if u != nil && u.Password != "" {
		hash = u.Password
	}
	comparison := compare([]byte(hash), []byte(suppliedPassword))
	if comparison != nil && !errors.Is(comparison, bcrypt.ErrMismatchedHashAndPassword) {
		// Malformed stored hashes must not create a fast rejection path either.
		_ = compare([]byte(missingAccountHash), []byte(suppliedPassword))
	}
	if u == nil || u.Password == "" || comparison != nil {
		return nil, errInvalidCredentials
	}
	return u, nil
}

func (a *EmailAuthService) changePassword(ctx context.Context, u *user.User, current, replacement string) error {
	if u == nil {
		return user.ErrUserNotSpecified
	}
	if u.Password != "" && !password.CheckPasswordHash(current, u.Password) {
		return errInvalidCredentials
	}

	hash, err := password.HashPassword(replacement)
	if err != nil {
		return err
	}
	return a.accounts.ChangePassword(ctx, u, hash)
}

func (a *EmailAuthService) resetPassword(ctx context.Context, u *user.User, token, replacement string, requireNew bool) error {
	if u == nil {
		return user.ErrUserNotSpecified
	}
	if requireNew && u.Password != "" && password.CheckPasswordHash(replacement, u.Password) {
		return errPasswordReused
	}

	hash, err := password.HashPassword(replacement)
	if err != nil {
		return err
	}
	return u.ResetPassword(ctx, a.accounts, token, []byte(a.config.Auth.JwtKey), hash)
}

func (a *EmailAuthService) sendRegistrationVerification(ctx context.Context, email string, origin url.URL) error {
	u, err := a.accounts.GetByEmail(ctx, email)
	if err != nil {
		return err
	}
	if u == nil {
		return user.ErrUserNotFound
	}
	return a.SendVerificationEmail(ctx, u, origin)
}

// Unknown and ineligible accounts require no delivery. Failures remain visible to
// the caller, which must give every valid request the same HTTP acknowledgment.
func (a *EmailAuthService) requestPasswordReset(ctx context.Context, email string, expiration time.Duration, link url.URL) error {
	u, err := a.accounts.GetByEmail(ctx, email)
	if errors.Is(err, user.ErrUserNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if u == nil || u.RegistrationAuthType != AuthTypeEmail {
		return nil
	}
	return a.sendResetEmail(ctx, u, expiration, link)
}

// SendVerificationEmail serves both registration and authenticated resend requests.
func (a *EmailAuthService) SendVerificationEmail(ctx context.Context, u *user.User, origin url.URL) error {
	if a.mailClient == nil {
		return messaging.ErrMailUnavailable
	}

	if u == nil || u.ID == "" {
		return errors.New("user not specified for verification email")
	}

	verificationEndpoint := a.config.Auth.VerificationEndpoint
	if verificationEndpoint == "" {
		verificationEndpoint = emailVerifyPath
	}

	tokenExpiration := a.config.Auth.VerificationTokenExpiration
	if tokenExpiration <= 0 {
		tokenExpiration = 24 * time.Hour
	}

	verificationToken, err := user.NewAuthVerificationToken(u, []byte(a.config.Auth.JwtKey), tokenExpiration)
	if err != nil {
		return err
	}

	verificationURL := url.URL{Scheme: origin.Scheme, Host: origin.Host, Path: verificationEndpoint}
	verificationURL.RawQuery = url.Values{"token": {verificationToken.Token}}.Encode()

	body, err := a.renderAccountEmail(a.verificationEmail, verificationURL.String())
	if err != nil {
		return err
	}

	if err := a.accounts.InsertToken(ctx, verificationToken.Metadata()); err != nil {
		return err
	}
	return a.mailClient.Compose().
		To(u.Email).
		Subject("Verify your account").
		Body(body).
		Send(ctx)
}

func (a *EmailAuthService) renderAccountEmail(t *template.Template, link string) (string, error) {
	var body bytes.Buffer
	if err := t.Execute(&body, struct{ Name, URL string }{
		Name: a.config.App.Name, URL: link,
	}); err != nil {
		return "", err
	}

	return body.String(), nil
}

func (a *EmailAuthService) sendResetEmail(ctx context.Context, u *user.User, expiration time.Duration, link url.URL) error {
	if a.mailClient == nil {
		return messaging.ErrMailUnavailable
	}

	token, err := user.NewAuthResetToken(u, []byte(a.config.Auth.JwtKey), expiration)
	if err != nil {
		return err
	}
	query := link.Query()
	query.Set("token", token.Token)
	link.RawQuery = query.Encode()

	body, err := a.renderAccountEmail(a.resetEmail, link.String())
	if err != nil {
		return err
	}

	if err := a.accounts.InsertToken(ctx, token.Metadata()); err != nil {
		return err
	}
	return a.mailClient.Compose().
		To(u.Email).
		Subject("Reset your password").
		Body(body).
		Send(ctx)
}
