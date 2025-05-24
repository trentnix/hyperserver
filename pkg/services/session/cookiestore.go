// cookiestore.go implements the SessionStore interface with a way to manage session
// exclusively via an HTTP cookie.
package session

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/dgrijalva/jwt-go"
	"github.com/trentnix/hyperserver/config"
)

type (
	// CookieStore serializes a Session to a browser cookie
	CookieStore struct {
		// key to encode the session data into a JWT
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

// Get retrieves a session from the specified request. If a session does not exist,
// an empty (new) session is returned even if an error has occurred.
func (c *CookieStore) Get(r *http.Request, name string) (*Session, error) {
	jwtValue := ""

	// check the request for a cookie
	cookie, errCookie := r.Cookie(name)
	if errCookie == nil {
		jwtValue = cookie.Value
	}

	if cookie == nil || jwtValue == "" {
		// if there is no cookie, return an empty session
		return newSession(c, name), nil
	}

	// return the decoded Session data
	claimsData, sessionError := parseSessionJWT(jwtValue, c.JwtKey)
	if sessionError != nil || jwtValue == "" {
		// if there is no session data or there was an error, return the new, empty session (and any error)
		return newSession(c, name), sessionError
	}

	// the session exists - overwrite the ID with the session data that was extracted from the cookie
	existingSession, loadErr := loadSession(claimsData.ID, name, claimsData.ExpiresAtTime(), c, claimsData.Value)
	if loadErr != nil {
		return newSession(c, name), loadErr
	}

	return existingSession, nil
}

// New creates an empty (new) session. An error will not be returned but the return
// requires an error to implement the SessionStore interface
func (c *CookieStore) New(r *http.Request, name string) (*Session, error) {
	return newSession(c, name), nil
}

// Save saves the specified Session to a token that can be added to a cookie
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
		ID:    session.ID,
		Value: sessionValue,
		StandardClaims: jwt.StandardClaims{
			ExpiresAt: tokenExpiration.UTC().Unix(),
			IssuedAt:  time.Now().UTC().Unix(),
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
	http.SetCookie(w, &http.Cookie{
		Name:     session.Name,
		Value:    tokenString,
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   cookieAge,
	})

	return nil
}

// End terminates the specified session by setting the corresponding session cookie
// to expired
func (c *CookieStore) End(w http.ResponseWriter, r *http.Request, session *Session) error {
	http.SetCookie(w, &http.Cookie{
		Name:     session.Name,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1, // Delete now
	})

	return nil
}

// IsEnabled informs the called whether the specified CookieStore is enabled and can be used
func (c *CookieStore) IsEnabled() bool {
	return c.enabled
}
