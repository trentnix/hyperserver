package session

import (
	"context"
	"net/http"

	"github.com/trentnix/hyperserver/pkg/requestinfo"
)

// ExpireCookie removes a session cookie from the browser without loading its data.
// It does not revoke stored sessions or clear the request-local session cache.
func ExpireCookie(w http.ResponseWriter, r *http.Request, name string) {
	http.SetCookie(w, &http.Cookie{
		Name: name, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: requestinfo.IsHTTPS(r), SameSite: http.SameSiteLaxMode,
	})
}

// SessionStore loads, creates, saves, and ends named sessions.
// Implementations must support concurrent requests and use r.Context() for I/O.
// Session values belong to one request and must not be shared concurrently.
// Providers must document persistence, concurrent-save, and revocation guarantees.
// Applications must reject providers that cannot meet their requirements.
type (
	SessionStore interface {
		// Get returns a stored session or a new one when absent or expired.
		// Invalid cookies, storage failures, and corrupt data return an error.
		// Expiration must be checked on reads, independently of cleanup. Separate
		// requests must receive independent session data.
		Get(r *http.Request, name string) (*Session, error)
		// New creates an unsaved session with a new ID.
		New(r *http.Request, name string) (*Session, error)
		// Save persists a session and writes its cookie before the response body.
		// Failure must not issue a cookie. Success does not guarantee that the
		// browser receives it. Saving must not extend an existing session's expiry.
		Save(w http.ResponseWriter, r *http.Request, session *Session) error
		// End ends a session according to the provider's revocation guarantees.
		End(w http.ResponseWriter, r *http.Request, session *Session) error
		// IsEnabled reports whether the store is enabled in configuration.
		IsEnabled() bool
	}
)

// ExpiredSessionMaintenance is an optional session-provider capability. A batch
// deletes at most limit expired sessions, preserves live sessions, and honors ctx.
type ExpiredSessionMaintenance interface {
	DeleteExpired(ctx context.Context, limit int) (int64, error)
}
