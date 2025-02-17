// cookiestore.go implements the SessionStore interface with a way to manage session
// as the value of a HTTP cookie
package session

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/dgrijalva/jwt-go"
	"github.com/trentnix/hyperserver/config"
)

type (
	// CookieStore implements the SessionStore interface
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

	// Claims contains the data that is serialized to and from a JWT
	CookieStoreSessionClaims struct {
		// identifies a unique session
		ID string `json:"id"`
		// values that can be stored in a session
		Data map[string]string `json:"data"`
		// stanard JWT claims embedded
		jwt.StandardClaims
	}
)

var ErrCookieStoreNotCreated = errors.New("a session store to store session data in a JWT in an HTTP cookie could not be created")

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

	configOptions := getStoreConfigOptions(c, sqliteStoreName)

	cookieStore := &CookieStore{
		JwtKey:         []byte(c.HTTP.Session.JwtKey),
		TokenLifetime:  c.HTTP.Session.TokenAge,
		CookieLifetime: c.HTTP.Session.CookieAge,
	}

	if strings.ToLower(configOptions["enabled"]) == "true" || strings.ToLower(configOptions["enabled"]) == "1" {
		cookieStore.enabled = true
	}

	return cookieStore, nil
}

// Get retrieves a session from the specified request. If a session does not exist,
// an empty (new) session is returned.
func (c *CookieStore) Get(r *http.Request, name string) (*Session, error) {
	session := newSession(c, name)

	jwtValue := ""

	// check the request for a cookie
	cookie, errCookie := r.Cookie(name)
	if errCookie == nil {
		jwtValue = cookie.Value
	}

	// return the decoded Session data
	claimsData, sessionError := c.parseJWT(jwtValue)
	if sessionError != nil || jwtValue == "" {
		// if there is no session data or there was an error, return the new, empty session (and any error)
		return session, sessionError
	}

	// the session exists - overwrite the ID with the session data that was extracted from the cookie
	session.ID = claimsData.ID
	session.Data = claimsData.Data
	session.IsNew = false

	return session, nil
}

// New creates an empty (new) session. An error will not be returned but the return
// requires an error to implement the SessionStore interface
func (c *CookieStore) New(r *http.Request, name string) (*Session, error) {
	return newSession(c, name), nil
}

// Save saves the specified Session to a token that can be added to a cookie
func (c *CookieStore) Save(r *http.Request, w http.ResponseWriter, s *Session) error {
	if len(c.JwtKey) == 0 {
		return ErrSessionKeyInvalid
	}

	expirationTime := time.Now().Add(c.TokenLifetime)
	claims := &CookieStoreSessionClaims{
		ID:   s.ID,
		Data: s.Data,
		StandardClaims: jwt.StandardClaims{
			ExpiresAt: expirationTime.Unix(),
			IssuedAt:  time.Now().Unix(),
		},
	}

	// Create and sign the token with the specified algorithm and claims
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(c.JwtKey)
	if err != nil {
		return errors.Join(ErrSessionKeyInvalid, err)
	}

	cookieAge := int(c.CookieLifetime)

	// write the cookie
	http.SetCookie(w, &http.Cookie{
		Name:     s.name,
		Value:    tokenString,
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil,
		MaxAge:   cookieAge,
	})

	return nil
}

// End terminates the specified session by setting the corresponding session cookie
// to expired
func (c *CookieStore) End(r *http.Request, w http.ResponseWriter, session *Session) error {
	http.SetCookie(w, &http.Cookie{
		Name:     session.name,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil,
		MaxAge:   -1, // Delete now
	})

	return nil
}

// parseJWT parses the specified token into a SessionClaims instance to extract
// session data
func (c *CookieStore) parseJWT(tokenString string) (*CookieStoreSessionClaims, error) {
	if tokenString == "" {
		return nil, ErrInvalidToken
	}

	// Parse and validate the JWT
	token, err := jwt.ParseWithClaims(tokenString, &CookieStoreSessionClaims{}, func(token *jwt.Token) (interface{}, error) {
		// Ensure the token is signed with the expected method
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrInvalidToken
		}
		return c.JwtKey, nil
	})
	if err != nil {
		return nil, err
	}

	// Extract claims
	if claims, ok := token.Claims.(*CookieStoreSessionClaims); ok && token.Valid {
		return claims, nil
	}

	return nil, ErrInvalidToken
}

// IsEnabled informs the called whether the specified CookieStore is enabled and can be used
func (c *CookieStore) IsEnabled() bool {
	return c.enabled
}
