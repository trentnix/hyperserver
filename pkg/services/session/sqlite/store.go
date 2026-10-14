// Package sqlite provides SQLite-backed sessions with signed identifier cookies.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/trentnix/hyperserver/pkg/database"
	sqlitedb "github.com/trentnix/hyperserver/pkg/database/sqlite"
	"github.com/trentnix/hyperserver/pkg/requestinfo"
	sessions "github.com/trentnix/hyperserver/pkg/services/session"
	"github.com/trentnix/hyperserver/pkg/services/session/internal/sessioncookie"
)

type (
	// SQLiteStore persists session data in SQLite and signs session IDs in cookies.
	// Each store owns its pool. Close it after its consumers stop.
	SQLiteStore struct {
		// Serializer selects the payload format. Nil uses JSON. Set it before use
		// and do not change it while requests are active.
		Serializer sessions.Serializer
		// JwtKey signs the session-ID cookie. It does not encrypt its contents.
		JwtKey []byte
		// lifetime of the JWT
		TokenLifetime time.Duration
		// lifetime of the cookie containing the JWT
		CookieLifetime time.Duration
		// table that will store sessions
		tableName string
		// database connection
		db *sql.DB
		// enabled
		enabled bool
	}

	// SQLiteSession is the database representation of an encoded session.
	SQLiteSession struct {
		ID         string
		Session    string
		Expires_at time.Time
		Created_at time.Time
		Updated_at time.Time
	}
)

var (
	// ErrSQLiteStoreNotConfigured indicates that SQLite session settings are missing.
	ErrSQLiteStoreNotConfigured = errors.New("the sqliteStore is not configured")
	// ErrDatabaseNotConfigured indicates that the session store has no database connection.
	ErrDatabaseNotConfigured = errors.New("the session database is not configured")
)

// Options configures a SQLite session store. Application setup decides whether
// to construct the store. A successful constructor always returns an enabled store.
type Options struct {
	// Connection is a SQLite connection string. Application setup must resolve its path.
	Connection string
	// Table names the session table to prepare and use.
	Table string
	// SigningKey signs session-ID cookies. It must not be empty or a placeholder.
	SigningKey string
	// TokenLifetime and CookieLifetime must each be at least one second.
	TokenLifetime  time.Duration
	CookieLifetime time.Duration
}

// NewSQLiteStore opens an owned pool and prepares the session table before use.
// Existing tables must have all required columns. Setup does not migrate them.
// Setup honors ctx and closes the pool on failure. The caller must close the store.
func NewSQLiteStore(ctx context.Context, options Options) (*SQLiteStore, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := sessioncookie.ValidateOptions(options.SigningKey, options.TokenLifetime, options.CookieLifetime); err != nil {
		return nil, err
	}
	if strings.TrimSpace(options.Connection) == "" || strings.TrimSpace(options.Table) == "" {
		return nil, ErrSQLiteStoreNotConfigured
	}

	db, err := sqlitedb.Open(options.Connection)
	if err != nil {
		return nil, err
	}
	sqliteStore := &SQLiteStore{
		JwtKey:         []byte(options.SigningKey),
		TokenLifetime:  options.TokenLifetime,
		CookieLifetime: options.CookieLifetime,
		tableName:      options.Table,
		db:             db,
		enabled:        true,
	}
	if err := sqliteStore.configureDatabase(ctx); err != nil {
		return nil, errors.Join(err, db.Close())
	}

	return sqliteStore, nil
}

// Get loads the session identified by the signed request cookie.
// A missing or expired cookie or stored session returns a new session. Invalid
// cookies, database failures, and corrupt unexpired data return an error.
func (s *SQLiteStore) Get(r *http.Request, name string) (*sessions.Session, error) {
	if s.db == nil {
		return nil, ErrDatabaseNotConfigured
	}

	// get the session ID from the request cookie
	jwtValue := ""

	// check the request for a cookie
	cookie, errCookie := r.Cookie(name)
	if errCookie == nil {
		jwtValue = cookie.Value
	}

	if cookie == nil {
		// if there is no cookie, return an empty session
		session := sessions.NewSession(s, name)
		return session, nil
	}
	if err := sessioncookie.Validate(cookie); err != nil {
		return nil, sessions.NewErrInvalidToken(err)
	}

	// return the decoded Session data
	claimsData, sessionError := sessioncookie.Parse(jwtValue, s.JwtKey)
	if errors.Is(sessionError, jwt.ErrTokenExpired) {
		return sessions.NewSession(s, name), nil
	}
	if sessionError != nil {
		return nil, sessionError
	}

	// get the session from the database
	sessionID := claimsData.ID
	session, sessionError := s.getSessionFromDatabase(r.Context(), sessionID, name)
	if sessionError != nil {
		// return an error
		return nil, sessionError
	}

	// if the session does not exist, return a new session
	if session == nil {
		session = sessions.NewSession(s, name)
	}

	return session, nil
}

// New creates a new session and returns the newly created Session instance
func (s *SQLiteStore) New(r *http.Request, name string) (*sessions.Session, error) {
	if s.db == nil {
		return nil, ErrDatabaseNotConfigured
	}

	return sessions.NewSession(s, name), nil
}

// Save stores session data and writes its signed identifier to an HTTP cookie.
// Updating a session does not extend its stored lifetime or recreate a deleted row.
func (s *SQLiteStore) Save(w http.ResponseWriter, r *http.Request, session *sessions.Session) error {
	if s.db == nil {
		return ErrDatabaseNotConfigured
	}

	cookie, err := s.sessionCookie(session, requestinfo.IsHTTPS(r))
	if err != nil {
		return err
	}
	if err := s.save(r.Context(), session); err != nil {
		return err
	}

	http.SetCookie(w, cookie)
	return nil
}

func (s *SQLiteStore) sessionCookie(session *sessions.Session, secure bool) (*http.Cookie, error) {
	var tokenExpiration time.Time

	if session.IsNew {
		tokenExpiration = time.Now().UTC().Add(s.TokenLifetime)
		session.ExpiresAt = tokenExpiration
	} else {
		tokenExpiration = session.ExpiresAt
	}

	claims := &sessioncookie.Claims{
		ID:      session.ID,
		Purpose: sessioncookie.Purpose,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(tokenExpiration),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}

	// Create and sign the token with the specified algorithm and claims
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(s.JwtKey)
	if err != nil {
		return nil, sessions.NewErrSessionKeyInvalid(err)
	}

	cookieAge := int(s.CookieLifetime / time.Second)

	cookie := &http.Cookie{
		Name:     session.Name,
		Value:    tokenString,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   cookieAge,
	}
	if err := sessioncookie.Validate(cookie); err != nil {
		return nil, err
	}
	return cookie, nil
}

// Rotate replaces the old row and inserts a fresh session in one transaction.
// A revoked or expired session cannot be rotated back into an active session.
func (s *SQLiteStore) Rotate(w http.ResponseWriter, r *http.Request, previous *sessions.Session, data map[string]any) (*sessions.Session, error) {
	if s.db == nil {
		return nil, ErrDatabaseNotConfigured
	}
	if s.tableName == "" {
		return nil, ErrSQLiteStoreNotConfigured
	}
	if previous == nil || previous.ID == "" {
		return nil, sessions.NewErrSessionInvalid(nil)
	}

	next := sessions.NewSession(s, previous.Name)
	for key, value := range data {
		next.Data[key] = value
	}
	encoded, err := next.EncodedData(s.Serializer)
	if err != nil {
		return nil, err
	}
	cookie, err := s.sessionCookie(next, requestinfo.IsHTTPS(r))
	if err != nil {
		return nil, err
	}

	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if !previous.IsNew {
		result, err := tx.ExecContext(r.Context(), "DELETE FROM "+quoteIdentifier(s.tableName)+" WHERE id = ? AND julianday(expires_at) > julianday(?)",
			previous.ID, time.Now())
		if err != nil {
			return nil, err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return nil, err
		}
		if count != 1 {
			return nil, sessions.NewErrSessionInvalid(nil)
		}
	}

	_, err = tx.ExecContext(r.Context(), "INSERT INTO "+quoteIdentifier(s.tableName)+" (id, session, expires_at, created_at, updated_at) VALUES (?, ?, ?, ?, ?)",
		next.ID, encoded, next.ExpiresAt, time.Now(), time.Now())
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	next.IsNew = false
	http.SetCookie(w, cookie)
	return next, nil
}

// End deletes the stored session before expiring the browser cookie. Missing rows
// are already revoked. A storage failure leaves the browser cookie unchanged.
func (s *SQLiteStore) End(w http.ResponseWriter, r *http.Request, session *sessions.Session) error {
	if s.db == nil {
		return ErrDatabaseNotConfigured
	}
	if s.tableName == "" {
		return ErrSQLiteStoreNotConfigured
	}
	if session == nil || session.ID == "" {
		return sessions.NewErrSessionInvalid(nil)
	}

	query := fmt.Sprintf("DELETE FROM %s WHERE id = ?", quoteIdentifier(s.tableName))
	if _, err := s.db.ExecContext(r.Context(), query, session.ID); err != nil {
		return err
	}

	sessions.ExpireCookie(w, r, session.Name)
	return nil
}

// IsEnabled reports whether this SQLite store was initialized for use.
func (s *SQLiteStore) IsEnabled() bool {
	return s.enabled
}

// Close releases the store's pool. Call it after requests and cleanup work finish.
func (s *SQLiteStore) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// configureDatabase creates missing storage and checks required columns.
// Column presence does not establish compatible types or stored values.
func (s *SQLiteStore) configureDatabase(ctx context.Context) error {
	createTableSQL := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id VARCHAR(36) PRIMARY KEY,
			session TEXT,
			expires_at DATETIME,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);`, quoteIdentifier(s.tableName))

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return database.NewErrDatabase(err)
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, createTableSQL)
	if err != nil {
		return database.NewErrDatabase(err)
	}

	query := fmt.Sprintf("SELECT id, session, expires_at, created_at, updated_at FROM %s LIMIT 0", quoteIdentifier(s.tableName))
	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return fmt.Errorf("check SQLite session schema columns in %q: %w", s.tableName, err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close SQLite session schema check: %w", err)
	}

	query = fmt.Sprintf("CREATE INDEX IF NOT EXISTS %s ON %s (julianday(expires_at), id)", quoteIdentifier(s.tableName+"_expiration"), quoteIdentifier(s.tableName))
	if _, err := tx.ExecContext(ctx, query); err != nil {
		return database.NewErrDatabase(err)
	}

	return tx.Commit()
}

// getSessionFromDatabase attempts to retrieve the specified session from the database
func (s *SQLiteStore) getSessionFromDatabase(ctx context.Context, sessionID string, name string) (*sessions.Session, error) {
	if s.db == nil {
		return nil, database.NewErrDatabaseUnavailable(nil)
	}

	if s.tableName == "" {
		return nil, ErrSQLiteStoreNotConfigured
	}

	var dbSession SQLiteSession

	// Read only a bounded prefix so corrupt oversized rows cannot allocate their
	// entire payload in Go. loadSession rejects the extra byte before decoding.
	query := fmt.Sprintf("SELECT id, substr(CAST(session AS BLOB), 1, ?) AS session, expires_at, created_at, updated_at FROM %s WHERE id = ?", quoteIdentifier(s.tableName))
	err := s.db.QueryRowContext(ctx, query, sessions.MaxEncodedSessionSize+1, sessionID).
		Scan(&dbSession.ID, &dbSession.Session, &dbSession.Expires_at, &dbSession.Created_at, &dbSession.Updated_at)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !dbSession.Expires_at.After(time.Now()) {
		return nil, nil
	}

	return sessions.LoadSession(dbSession.ID, name, dbSession.Expires_at, s, dbSession.Session, s.Serializer)
}

// saveSessionToDatabase creates or updates the specified session in the database
func (s *SQLiteStore) save(ctx context.Context, session *sessions.Session) error {
	if s.db == nil {
		return database.NewErrDatabaseUnavailable(nil)
	}

	valueData, err := session.EncodedData(s.Serializer)
	if err != nil {
		return err
	}

	dbSession := &SQLiteSession{
		ID:         session.ID,
		Session:    valueData,
		Expires_at: session.ExpiresAt,
	}

	var dbErr error
	if session.IsNew {
		dbErr = s.create(ctx, dbSession)
	} else {
		dbErr = s.update(ctx, dbSession)
	}

	if dbErr != nil {
		return dbErr
	}

	session.IsNew = false
	return nil
}

// create creates a new sessions table entry
func (s *SQLiteStore) create(ctx context.Context, session *SQLiteSession) error {
	if session.ID == "" {
		return sessions.NewErrSessionInvalid(nil)
	}

	session.Created_at = time.Now()
	session.Updated_at = session.Created_at

	query := fmt.Sprintf("INSERT INTO %s (id, session, expires_at, created_at, updated_at) VALUES (?, ?, ?, ?, ?)", quoteIdentifier(s.tableName))
	_, err := s.db.ExecContext(ctx, query, session.ID, session.Session, session.Expires_at, session.Created_at, session.Updated_at)

	return err
}

// update updates the specified session in the sessions table
func (s *SQLiteStore) update(ctx context.Context, session *SQLiteSession) error {
	if session.ID == "" {
		return sessions.NewErrSessionInvalid(nil)
	}

	session.Updated_at = time.Now()

	// Saving data must not extend stored expiry or recreate a revoked session.
	query := fmt.Sprintf("UPDATE %s SET session = ?, updated_at = ? WHERE id = ?", quoteIdentifier(s.tableName))
	result, err := s.db.ExecContext(ctx, query, session.Session, session.Updated_at, session.ID)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return sessions.NewErrSessionInvalid(nil)
	}
	return nil
}

// quoteIdentifier treats configured names as single SQLite identifiers.
func quoteIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}
