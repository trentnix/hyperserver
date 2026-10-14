// Package cookie stores session data in signed browser cookies.
package cookie

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/trentnix/hyperserver/pkg/requestinfo"
	sessions "github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/session/internal/sessioncookie"
)

type (
	// CookieStore serializes a Session to a browser cookie
	CookieStore struct {
		// Serializer selects the payload format. Nil uses JSON. Set it before use
		// and do not change it while requests are active.
		Serializer sessions.Serializer
		// JwtKey signs session JWTs. It does not encrypt their contents.
		JwtKey []byte
		// lifetime of the JWT
		TokenLifetime time.Duration
		// lifetime of the cookie containing the JWT
		CookieLifetime time.Duration

		// enabled
		enabled bool
	}
)

// Options configures a cookie session store. Application setup decides whether
// to construct the store. A successful constructor always returns an enabled store.
type Options struct {
	// SigningKey signs session cookies. It must not be empty or a placeholder.
	SigningKey string
	// TokenLifetime and CookieLifetime must each be at least one second.
	TokenLifetime  time.Duration
	CookieLifetime time.Duration
}

// NewCookieStore validates options and returns a ready-to-use cookie store.
func NewCookieStore(options Options) (*CookieStore, error) {
	if err := sessioncookie.ValidateOptions(options.SigningKey, options.TokenLifetime, options.CookieLifetime); err != nil {
		return nil, err
	}

	return &CookieStore{
		JwtKey:         []byte(options.SigningKey),
		TokenLifetime:  options.TokenLifetime,
		CookieLifetime: options.CookieLifetime,
		enabled:        true,
	}, nil
}

// Get returns a stored session or a new session if the cookie is missing or expired.
// Invalid cookies and decoding failures return an error without a session.
func (c *CookieStore) Get(r *http.Request, name string) (*sessions.Session, error) {
	jwtValue := ""

	// check the request for a cookie
	cookie, errCookie := r.Cookie(name)
	if errCookie == nil {
		jwtValue = cookie.Value
	}

	if cookie == nil {
		// if there is no cookie, return an empty session
		return sessions.NewSession(c, name), nil
	}
	if err := sessioncookie.Validate(cookie); err != nil {
		return nil, sessions.NewErrInvalidToken(err)
	}

	// return the decoded Session data
	claimsData, sessionError := sessioncookie.Parse(jwtValue, c.JwtKey)
	if errors.Is(sessionError, jwt.ErrTokenExpired) {
		return sessions.NewSession(c, name), nil
	}
	if sessionError != nil {
		return nil, sessionError
	}

	// the session exists - overwrite the ID with the session data that was extracted from the cookie
	existingSession, loadErr := sessions.LoadSession(claimsData.ID, name, claimsData.ExpiresAtTime(), c, claimsData.Value, c.Serializer)
	if loadErr != nil {
		return nil, loadErr
	}

	return existingSession, nil
}

// New creates an empty (new) session. An error will not be returned but the return
// requires an error to implement the SessionStore interface
func (c *CookieStore) New(r *http.Request, name string) (*sessions.Session, error) {
	return sessions.NewSession(c, name), nil
}

// Save encodes the session into a signed JWT and writes an HTTP cookie.
// The payload is not encrypted and must not contain secrets. Cookies larger than
// MaxCookieSize are rejected. Call Save before writing response headers or a body.
func (c *CookieStore) Save(w http.ResponseWriter, r *http.Request, session *sessions.Session) error {
	if len(c.JwtKey) == 0 {
		return sessions.NewErrSessionKeyInvalid(fmt.Errorf("the JWT key is not set"))
	}

	// create a token with just the session (and expiration)
	var tokenExpiration time.Time

	if session.IsNew {
		tokenExpiration = time.Now().UTC().Add(c.TokenLifetime)
		session.ExpiresAt = tokenExpiration
	} else {
		tokenExpiration = session.ExpiresAt
	}

	sessionValue, err := session.EncodedData(c.Serializer)
	if err != nil {
		return err
	}

	claims := &sessioncookie.Claims{
		ID:      session.ID,
		Purpose: sessioncookie.Purpose,
		Value:   sessionValue,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(tokenExpiration),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}

	// Create and sign the token with the specified algorithm and claims
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(c.JwtKey)
	if err != nil {
		return sessions.NewErrSessionKeyInvalid(err)
	}

	cookieAge := int(c.CookieLifetime / time.Second)

	// write the cookie
	cookie := &http.Cookie{
		Name:     session.Name,
		Value:    tokenString,
		Path:     "/",
		HttpOnly: true,
		Secure:   requestinfo.IsHTTPS(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   cookieAge,
	}
	if err := sessioncookie.Validate(cookie); err != nil {
		return err
	}
	http.SetCookie(w, cookie)

	return nil
}

// End expires the browser cookie. It cannot revoke copies of a signed cookie.
func (c *CookieStore) End(w http.ResponseWriter, r *http.Request, session *sessions.Session) error {
	sessions.ExpireCookie(w, r, session.Name)
	return nil
}

// IsEnabled reports whether this cookie store was initialized for use.
func (c *CookieStore) IsEnabled() bool {
	return c.enabled
}
