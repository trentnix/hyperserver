// session.go wraps session management so that whether a JWT or server-managed session is used is
// abstracted from the caller. Currently, the implementation uses a JWT but this could be abstracted
// so that the SessionManager uses a single interface irrespective of how a session is managed
package session

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/dgrijalva/jwt-go"
	"github.com/trentnix/hyperserver/pkg/services/logger"
)

type (
	// SessionManager is used to manage a session
	SessionManager struct {
		JwtKey         []byte
		TokenLifetime  time.Duration
		cookieSettings *CookieSettings
	}

	// Settings for the cookie that will be written and read from the http.Writer and http.Response, respectively
	CookieSettings struct {
		Name   string
		MaxAge int
	}

	// Claims contains the data that is serialized to and from a JWT
	Claims struct {
		SessionData any `json:"SessionData"`
		jwt.StandardClaims
	}
)

var (
	ErrSessionKeyInvalid = errors.New("the session key is invalid")
	ErrSessionNotFound   = errors.New("session not found")
	ErrSessionInvalid    = errors.New("the session is invalid")
)

// NewSessionManager creates a new instance of a SessionManager object with default values for
// TokenLifetime and cookie name
func NewSessionManager(jwtKey []byte) *SessionManager {
	return &SessionManager{
		JwtKey:        jwtKey,
		TokenLifetime: time.Hour * 24,
		cookieSettings: &CookieSettings{
			Name:   "auth-token",
			MaxAge: 86400,
		},
	}
}

// SetSessionData takes the specified data and adds it to a session
func (s *SessionManager) SetSessionData(sessionData any) (string, error) {
	if len(s.JwtKey) == 0 {
		return "", ErrSessionKeyInvalid
	}

	expirationTime := time.Now().Add(s.TokenLifetime)
	claims := &Claims{
		SessionData: sessionData,
		StandardClaims: jwt.StandardClaims{
			ExpiresAt: expirationTime.Unix(),
		},
	}

	// Create and sign the token with the specified algorithm and claims
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(s.JwtKey)
	if err != nil {
		return "", errors.Join(ErrSessionKeyInvalid, err)
	}

	return tokenString, nil
}

// GetSessionData retrieves the data that was stored in the session
func (s *SessionManager) GetSessionData(r *http.Request) (interface{}, bool) {
	sessionData, err := s.getSessionValue(r, "SessionData")
	if err != nil {
		if !errors.Is(err, http.ErrNoCookie) {
			logger.LogRequestError(r, "There was an error retrieving SessionData from the request.", err, 0)
		}

		return nil, false
	}

	return sessionData, true
}

// StartSession creates a new session
func (s *SessionManager) StartSession(w http.ResponseWriter, r *http.Request, token string) error {
	http.SetCookie(w, &http.Cookie{
		Name:     s.cookieSettings.Name,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil,
		MaxAge:   s.cookieSettings.MaxAge,
	})

	return nil
}

// EndSession ends the specified named session
func (s *SessionManager) EndSession(w http.ResponseWriter, r *http.Request) error {
	http.SetCookie(w, &http.Cookie{
		Name:     s.cookieSettings.Name,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil,
		MaxAge:   -1, // Delete now
	})

	return nil
}

// getSessionValue extracts the specified value from the session, if the session is valid
func (s *SessionManager) getSessionValue(r *http.Request, key string) (interface{}, error) {
	cookie, err := r.Cookie(s.cookieSettings.Name)
	if err != nil {
		return nil, errors.Join(ErrSessionNotFound, err)
	}

	token, err := jwt.Parse(cookie.Value, func(token *jwt.Token) (interface{}, error) {
		return s.JwtKey, nil
	})
	if err != nil || !token.Valid {
		return nil, errors.Join(ErrSessionInvalid, err)
	}

	if claims, ok := token.Claims.(jwt.MapClaims); ok {
		if val, ok := claims[key]; ok {
			return val, nil
		}

		return nil, errors.Join(ErrSessionNotFound, fmt.Errorf("key %q not found in session", key))
	}

	return nil, ErrSessionInvalid
}
