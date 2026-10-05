package session

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/requestinfo"
)

type (
	// CookieStore serializes a Session to a browser cookie
	CookieStore struct {
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

const (
	cookieStoreName = "CookieStore"
)

// NewCookieStore returns an instance of a CookieStore that can be used
// to get and save a session
func NewCookieStore(c *config.Config) (*CookieStore, error) {
	configErr := checkStoreCookieConfig(c)
	if configErr != nil {
		return nil, configErr
	}

	configOptions := getStoreConfigOptions(c, cookieStoreName)

	cookieStore := &CookieStore{
		JwtKey:         []byte(c.HTTP.Session.JwtKey),
		TokenLifetime:  c.HTTP.Session.TokenAge,
		CookieLifetime: c.HTTP.Session.CookieAge,
	}

	enabled, ok := configOptions["enabled"]
	if ok {
		enabled = strings.ToLower(enabled)
		if enabled == "true" || enabled == "1" {
			cookieStore.enabled = true
		}
	}

	return cookieStore, nil
}

// Get returns a stored session or a new session if the cookie is missing or expired.
// Invalid cookies and decoding failures return an error without a session.
func (c *CookieStore) Get(r *http.Request, name string) (*Session, error) {
	jwtValue := ""

	// check the request for a cookie
	cookie, errCookie := r.Cookie(name)
	if errCookie == nil {
		jwtValue = cookie.Value
	}

	if cookie == nil {
		// if there is no cookie, return an empty session
		return newSession(c, name), nil
	}
	if err := validateSessionCookie(cookie); err != nil {
		return nil, NewErrInvalidToken(err)
	}

	// return the decoded Session data
	claimsData, sessionError := parseSessionJWT(jwtValue, c.JwtKey)
	if errors.Is(sessionError, jwt.ErrTokenExpired) {
		return newSession(c, name), nil
	}
	if sessionError != nil {
		return nil, sessionError
	}

	// the session exists - overwrite the ID with the session data that was extracted from the cookie
	existingSession, loadErr := loadSession(claimsData.ID, name, claimsData.ExpiresAtTime(), c, claimsData.Value)
	if loadErr != nil {
		return nil, loadErr
	}

	return existingSession, nil
}

// New creates an empty (new) session. An error will not be returned but the return
// requires an error to implement the SessionStore interface
func (c *CookieStore) New(r *http.Request, name string) (*Session, error) {
	return newSession(c, name), nil
}

// Save encodes the session into a signed JWT and writes an HTTP cookie.
// The payload is not encrypted and must not contain secrets. Cookies larger than
// MaxCookieSize are rejected. Call Save before writing response headers or a body.
func (c *CookieStore) Save(w http.ResponseWriter, r *http.Request, session *Session) error {
	if len(c.JwtKey) == 0 {
		return NewErrSessionKeyInvalid(fmt.Errorf("the JWT key is not set"))
	}

	// create a token with just the session (and expiration)
	var tokenExpiration time.Time

	if session.IsNew {
		tokenExpiration = time.Now().UTC().Add(c.TokenLifetime)
		session.ExpiresAt = tokenExpiration
	} else {
		tokenExpiration = session.ExpiresAt
	}

	sessionValue, err := session.EncodedData()
	if err != nil {
		return err
	}

	claims := &SessionClaims{
		ID:      session.ID,
		Purpose: sessionTokenPurpose,
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
		return NewErrSessionKeyInvalid(err)
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
	if err := validateSessionCookie(cookie); err != nil {
		return err
	}
	http.SetCookie(w, cookie)

	return nil
}

// End expires the browser cookie. It cannot revoke copies of a signed cookie.
func (c *CookieStore) End(w http.ResponseWriter, r *http.Request, session *Session) error {
	ExpireCookie(w, r, session.Name)
	return nil
}

// IsEnabled reports whether this cookie store is enabled in configuration.
func (c *CookieStore) IsEnabled() bool {
	return c.enabled
}
