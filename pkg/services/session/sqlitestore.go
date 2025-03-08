// sqlitestore.go defines an implementation of the SessionStore interface that
// stores session data in SQLite
package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/dgrijalva/jwt-go"
	"github.com/jmoiron/sqlx"
	"github.com/mattn/go-sqlite3"
	"github.com/trentnix/hyperserver/config"
	"github.com/trentnix/hyperserver/pkg/database"
)

type (
	SQLiteStore struct {
		// key to encode the session data into a JWT
		JwtKey []byte
		// lifetime of the JWT
		TokenLifetime time.Duration
		// lifetime of the cookie containing the JWT
		CookieLifetime time.Duration
		// database connection string
		connectionString string
		// table that will store sessions
		tableName string
		// database connection
		db *sqlx.DB
		// enabled
		enabled bool
	}

	SQLiteSession struct {
		ID         string    `db:"id"`
		Session    string    `db:"session"`
		Expires_at time.Time `db:"expires_at"`
		Created_at time.Time `db:"created_at"`
		Updated_at time.Time `db:"updated_at"`
	}
)

var (
	dbSQLiteSessionStore *sqlx.DB
	dbOnce               sync.Once

	ErrSQLiteStoreNotConfigured = errors.New("the sqliteStore is not configured")
	ErrDatabaseNotConfigured    = errors.New("the session database is not configured")
)

const (
	sqliteDriver    = "sqlite3"
	sqliteStoreName = "SQLiteStore"
)

// NewSQLiteStore configures the database that will be used to store session data and
// creates a new instance of SQLiteStore that can be used to store and retrieve session
// data. If the SQL database is not configured correctly or setup fails, an error is
// returned.
func NewSQLiteStore(c *config.Config) (*SQLiteStore, error) {
	configErr := checkStoreCookieConfig(c)
	if configErr != nil {
		return nil, configErr
	}

	configOptions := getStoreConfigOptions(c, sqliteStoreName)

	sqliteStore := &SQLiteStore{
		JwtKey:           []byte(c.HTTP.Session.JwtKey),
		TokenLifetime:    c.HTTP.Session.TokenAge,
		CookieLifetime:   c.HTTP.Session.CookieAge,
		connectionString: configOptions["connection"],
		tableName:        configOptions["sessiontable"],
		enabled:          strings.EqualFold(configOptions["enabled"], "true") || configOptions["enabled"] == "1",
	}

	dbErr := sqliteStore.prepareDatabase()
	if dbErr != nil {
		return nil, dbErr
	}

	return sqliteStore, nil
}

// Get retrieves any session information from the request Cookie and,
func (s *SQLiteStore) Get(r *http.Request, name string) (*Session, error) {
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
		session := newSession(s, name)
		return session, nil
	}

	// return the decoded Session data
	claimsData, sessionError := parseSessionJWT(jwtValue, s.JwtKey)
	if sessionError != nil || jwtValue == "" {
		// if there is no session data or there was an error, return a new
		// session (and the error)
		session := newSession(s, name)
		return session, sessionError
	}

	// get the session from the database
	sessionID := claimsData.ID
	session, sessionError := s.getSessionFromDatabase(sessionID, name)
	if sessionError != nil {
		// return an error
		return nil, sessionError
	}

	// if the session does not exist, return a new session
	if session == nil {
		session := newSession(s, name)
		return session, nil
	}

	session.IsNew = false
	return session, nil
}

// New creates a new session and returns the newly created Session instance
func (s *SQLiteStore) New(r *http.Request, name string) (*Session, error) {
	if s.db == nil {
		return nil, ErrDatabaseNotConfigured
	}

	return newSession(s, name), nil
}

// Save serializes specified Session to the database and saves the session identifier
// to an HTTP cookie
func (s *SQLiteStore) Save(r *http.Request, w http.ResponseWriter, session *Session) error {
	if s.db == nil {
		return ErrDatabaseNotConfigured
	}

	// create a token with just the session (and expiration)
	var tokenExpiration time.Time

	if session.IsNew {
		tokenExpiration = time.Now().UTC().Add(s.TokenLifetime)
		session.ExpiresAt = tokenExpiration
	} else {
		tokenExpiration = session.ExpiresAt
	}

	claims := &SessionClaims{
		ID: session.ID,
		StandardClaims: jwt.StandardClaims{
			ExpiresAt: tokenExpiration.UTC().Unix(),
			IssuedAt:  time.Now().UTC().Unix(),
		},
	}

	// Create and sign the token with the specified algorithm and claims
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(s.JwtKey)
	if err != nil {
		return errors.Join(ErrSessionKeyInvalid, err)
	}

	cookieAge := int(s.CookieLifetime / time.Second)

	// save the session to the database
	saveErr := s.save(session)
	if saveErr != nil {
		return saveErr
	}

	// write the cookie with the session token
	http.SetCookie(w, &http.Cookie{
		Name:     session.name,
		Value:    tokenString,
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil,
		MaxAge:   cookieAge,
	})

	return nil
}

// End terminates the specified session
func (s *SQLiteStore) End(r *http.Request, w http.ResponseWriter, session *Session) error {
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

// IsEnabled informs the called whether the specified CookieStore is enabled and can be used
func (s *SQLiteStore) IsEnabled() bool {
	return s.enabled
}

// validateDatabase determines whether the sessionsTable exists and, if not, it creates it
func (s *SQLiteStore) prepareDatabase() error {
	if dbSQLiteSessionStore != nil {
		s.db = dbSQLiteSessionStore
		return nil
	}

	var dbError error
	dbOnce.Do(func() {
		s.db, dbError = database.Setup(sqliteDriver, s.connectionString)
		if dbError != nil {
			return
		}

		tableExists, err := database.TableExists(s.db.DB, s.tableName)
		if err != nil {
			dbError = database.NewErrDatabaseConfiguration(err)
			return
		}

		if !tableExists {
			err = s.configureDatabase()
			if err != nil {
				dbError = database.NewErrDatabase(err)
				return
			}
		}

		dbSQLiteSessionStore = s.db
	})

	if dbError != nil {
		return dbError
	}

	return nil
}

// configureDatabase creates the sessionTable according to the specified database vendor
func (s *SQLiteStore) configureDatabase() error {
	var createTableSQL string

	// Detect the database type by inspecting the driver
	driver := s.db.Driver()

	switch driver.(type) {
	case *sqlite3.SQLiteDriver:
		createTableSQL = fmt.Sprintf(`
			CREATE TABLE %s (
				id VARCHAR(36) PRIMARY KEY,
				session TEXT,
				expires_at DATETIME,
				created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
				updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
			);`, s.tableName)
	default:
		return database.NewErrDatabaseNotSupported(fmt.Errorf("the database type %T is not supported", s.db.Driver()))
	}

	_, err := s.db.Exec(createTableSQL)
	if err != nil {
		return database.NewErrDatabase(err)
	}

	return nil
}

// getSessionFromDatabase attempts to retrieve the specified session from the database
func (s *SQLiteStore) getSessionFromDatabase(sessionID string, name string) (*Session, error) {
	if s.db == nil {
		return nil, database.NewErrDatabaseUnavailable(nil)
	}

	if s.tableName == "" {
		return nil, ErrSQLiteStoreNotConfigured
	}

	var dbSession SQLiteSession

	query := fmt.Sprintf("SELECT id, session, expires_at, created_at, updated_at FROM %s WHERE id = ?", s.tableName)
	err := s.db.Get(&dbSession, query, sessionID)
	if err != nil {
		return nil, err
	}

	if dbSession.ID == sessionID {
		// create a new session and serialize the session data
		session := newSession(s, name)
		session.IsNew = false

		sessionData, dataError := convertJsonToData(dbSession.Session)
		if dataError != nil {
			// the JSON text blob that was stored could not be serialized to a map[string]string
			return nil, dataError
		}

		session.Data = sessionData

		// the session was retrieved and can be returned successfully
		return session, nil
	}

	return nil, nil
}

// saveSessionToDatabase creates or updates the specified session in the database
func (s *SQLiteStore) save(session *Session) error {
	if s.db == nil {
		return database.NewErrDatabaseUnavailable(nil)
	}

	jsonData, err := convertDataToJson(session.Data)
	if err != nil {
		return err
	}

	dbSession := &SQLiteSession{
		ID:         session.ID,
		Session:    jsonData,
		Expires_at: session.ExpiresAt,
	}

	var dbErr error
	if session.IsNew {
		dbErr = s.create(dbSession)
	} else {
		dbErr = s.update(dbSession)
	}

	if dbErr != nil {
		return dbErr
	}

	session.IsNew = false
	return nil
}

// create creates a new sessions table entry
func (s *SQLiteStore) create(session *SQLiteSession) error {
	if session.ID == "" {
		return ErrSessionInvalid
	}

	session.Created_at = time.Now()
	session.Updated_at = session.Created_at

	query := fmt.Sprintf("INSERT INTO %s (id, session, expires_at, updated_at, created_at) VALUES (:id, :session, :expires_at, :created_at, :updated_at)", s.tableName)
	_, err := s.db.NamedExec(query, session)

	return err
}

// update updates the specified session in the sessions table
func (s *SQLiteStore) update(session *SQLiteSession) error {
	if session.ID == "" {
		return ErrSessionInvalid
	}

	session.Updated_at = time.Now()

	query := fmt.Sprintf("UPDATE %s SET session = :session, expires_at = :expires_at, updated_at = :updated_at WHERE id = :id", s.tableName)
	_, err := s.db.NamedExec(query, session)

	return err
}

// end updates the database to terminate a session by setting the expiration time
// to the current time
func (s *SQLiteStore) end(session *SQLiteSession) error {
	if session.ID == "" {
		return ErrSessionInvalid
	}

	query := fmt.Sprintf("UPDATE %s SET expires_at = :expires_at WHERE id = :id", s.tableName)
	_, err := s.db.NamedExec(query, map[string]interface{}{
		"expires_at": time.Now(),
		"id":         session.ID,
	})

	return err
}

// delete deletes the specified session from the sessions table
func (s *SQLiteStore) delete(session *SQLiteSession) error {
	if session.ID == "" {
		return ErrSessionInvalid
	}

	query := fmt.Sprintf("DELETE FROM %s WHERE id = ?", s.tableName)
	_, err := s.db.Exec(query, session.ID)

	return err
}

// convertDataToJson takes a map[string]string and converts it to JSON
func convertDataToJson(data map[string]string) (string, error) {
	// serialize to JSON
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return "", err
	}

	// convert to string
	jsonString := string(jsonBytes)

	return jsonString, nil
}

// convertJsonToData takes a string of json values and converts them to a map[string]string
func convertJsonToData(jsonString string) (map[string]string, error) {
	var data map[string]string

	// unmarshal the JSON into the map
	err := json.Unmarshal([]byte(jsonString), &data)
	if err != nil {
		fmt.Println("Error decoding JSON:", err)
		return nil, err
	}

	return data, nil
}
