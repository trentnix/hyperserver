package session

import (
	"database/sql"
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

	// Claims contains the data that is serialized to and from a JWT
	SQLiteStoreSessionClaims struct {
		// identifies a unique session
		ID string `json:"id"`
		// standard JWT claims embedded
		jwt.StandardClaims
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
	once                    sync.Once
	sqliteStoreDbConfigured bool

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
	}

	if strings.ToLower(configOptions["enabled"]) == "true" || strings.ToLower(configOptions["enabled"]) == "1" {
		sqliteStore.enabled = true
	}

	var dbInitError error
	sqliteStore.db, dbInitError = database.Setup(sqliteDriver, sqliteStore.connectionString)
	if dbInitError != nil {
		return nil, dbInitError
	}

	once.Do(func() {
		// configure the database
		dbInitError = ensureDatabaseConfiguration(sqliteStore.db, sqliteStore.tableName)
	})

	if dbInitError != nil {
		return nil, dbInitError
	}

	sqliteStoreDbConfigured = true

	return sqliteStore, nil
}

// Get retrieves any session information from the request Cookie and,
func (s *SQLiteStore) Get(r *http.Request, name string) (*Session, error) {
	if !sqliteStoreDbConfigured {
		return nil, ErrDatabaseNotConfigured
	}

	// get the session ID from the request cookie
	jwtValue := ""

	// check the request for a cookie
	cookie, errCookie := r.Cookie(name)
	if errCookie == nil {
		jwtValue = cookie.Value
	}

	// return the decoded Session data
	claimsData, sessionError := s.parseJWT(jwtValue)
	if sessionError != nil || jwtValue == "" {
		// if there is no session data or there was an error, return the
		// new, empty session (and any error)
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

// New creates a new session and returns the newly created Session instance
func (s *SQLiteStore) New(r *http.Request, name string) (*Session, error) {
	if !sqliteStoreDbConfigured {
		return nil, ErrDatabaseNotConfigured
	}

	return newSession(s, name), nil
}

// Save serializes specified Session to the database and saves the session identifier
// to an HTTP cookie
func (s *SQLiteStore) Save(r *http.Request, w http.ResponseWriter, session *Session) error {
	if !sqliteStoreDbConfigured {
		return ErrDatabaseNotConfigured
	}

	// create a token with just the session (and expiration)
	var tokenExpiration time.Time

	if session.IsNew {
		tokenExpiration = time.Now().UTC().Add(s.TokenLifetime)
	} else {
		tokenExpiration = session.ExpiresAt
	}

	claims := &SQLiteStoreSessionClaims{
		ID: session.ID,
		StandardClaims: jwt.StandardClaims{
			ExpiresAt: tokenExpiration.UTC().Unix(),
			IssuedAt:  time.Now().UTC().Unix(),
		},
	}

	session.ExpiresAt = tokenExpiration

	// Create and sign the token with the specified algorithm and claims
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(s.JwtKey)
	if err != nil {
		return errors.Join(ErrSessionKeyInvalid, err)
	}

	cookieAge := int(s.CookieLifetime / time.Second)

	// save the session to the database
	saveErr := s.saveSessionToDatabase(session)
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

// saveSessionToDatabase creates or updates the specified session in the database
func (s *SQLiteStore) saveSessionToDatabase(session *Session) error {
	if s.db == nil {
		return database.NewErrDatabaseUnavailable(nil)
	}

	jsonData, err := convertDataToJson(session.Data)
	if err != nil {
		return err
	}

	dbSession := SQLiteSession{
		ID:         session.ID,
		Session:    jsonData,
		Expires_at: session.ExpiresAt,
	}

	var dbErr error
	if session.IsNew {
		dbErr = dbSession.Create(s.db, s.tableName, session)
	} else {
		dbErr = dbSession.Update(s.db, s.tableName, session)
	}

	if dbErr != nil {
		return dbErr
	}

	session.IsNew = false
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

// ensureDatabaseConfiguration determines whether the sessionsTable exists and,
// if not, it creates it
func ensureDatabaseConfiguration(db *sqlx.DB, sessionsTable string) error {
	tableExists, err := sessionsTableExists(db, sessionsTable)
	if err != nil {
		return database.NewErrDatabaseConfiguration(err)
	}

	if !tableExists {
		err = createSessionsTable(db, sessionsTable)
		if err != nil {
			return database.NewErrDatabase(err)
		}
	}

	return nil
}

// sessionTableExists determines whether the sessionsTable exists
func sessionsTableExists(db *sqlx.DB, sessionsTable string) (bool, error) {
	var query string
	var args []interface{}

	// detect the database type by inspecting the driver
	driver := db.Driver()

	switch driver.(type) {
	case *sqlite3.SQLiteDriver:
		query = "SELECT name FROM sqlite_master WHERE type='table' AND name=?;"
		args = []interface{}{sessionsTable}
	default:
		// the database isn't a sqlite database
		return false, database.NewErrDatabaseNotSupported(nil)
	}

	var result string
	err := db.QueryRow(query, args...).Scan(&result)
	if err == sql.ErrNoRows {
		// the specified table does not exist
		return false, nil
	} else if err != nil {
		// some other error occurred
		return false, database.NewErrDatabase(err)
	}

	return true, nil
}

// createSessionTable creates the sessionTable according to the specified database vendor
func createSessionsTable(db *sqlx.DB, sessionTable string) error {
	var createTableSQL string

	// Detect the database type by inspecting the driver
	driver := db.Driver()

	switch driver.(type) {
	case *sqlite3.SQLiteDriver:
		createTableSQL = fmt.Sprintf(`
			CREATE TABLE %s (
				id VARCHAR(36) PRIMARY KEY,
				session TEXT,
				expires_at DATETIME,
				created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
				updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
			);`, sessionTable)
	default:
		return database.NewErrDatabaseNotSupported(fmt.Errorf("the database type %T is not supported", db.Driver()))
	}

	_, err := db.Exec(createTableSQL)
	if err != nil {
		return database.NewErrDatabase(err)
	}

	return nil
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

// parseJWT parses the specified token into a SessionClaims instance to extract
// session data
func (c *SQLiteStore) parseJWT(tokenString string) (*SQLiteStoreSessionClaims, error) {
	if tokenString == "" {
		return nil, ErrInvalidToken
	}

	// Parse and validate the JWT
	token, err := jwt.ParseWithClaims(tokenString, &SQLiteStoreSessionClaims{}, func(token *jwt.Token) (interface{}, error) {
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
	if claims, ok := token.Claims.(*SQLiteStoreSessionClaims); ok && token.Valid {
		return claims, nil
	}

	return nil, ErrInvalidToken
}

// Create creates a new sessions table entry
func (s *SQLiteSession) Create(db *sqlx.DB, table string, session *Session) error {
	if session.ID == "" {
		return ErrSessionInvalid
	}

	s.ID = session.ID
	s.Created_at = time.Now()
	s.Updated_at = time.Now()
	s.Expires_at = session.ExpiresAt

	query := fmt.Sprintf("INSERT INTO %s (id, session, expires_at, updated_at, created_at) VALUES (:id, :session, :expires_at, :created_at, :updated_at)", table)
	_, err := db.NamedExec(query, s)

	return err
}

// Update updates the specified session in the sessions table
func (s *SQLiteSession) Update(db *sqlx.DB, table string, session *Session) error {
	if session.ID == "" {
		return ErrSessionInvalid
	}

	s.ID = session.ID
	s.Created_at = time.Now()
	s.Updated_at = time.Now()

	query := fmt.Sprintf("UPDATE %s SET session = :session, expires_at = :expires_at, updated_at = :updated_at, created_at = :created_at WHERE id = :id", table)
	_, err := db.NamedExec(query, s)

	return err
}

// End updates the database to terminate a session by setting the expiration time to the current time
func (s *SQLiteSession) End(db *sqlx.DB, table string, session *Session) error {
	if session.ID == "" {
		return ErrSessionInvalid
	}

	query := fmt.Sprintf("UPDATE %s SET expires_at = :expires_at WHERE id = :id", table)
	_, err := db.NamedExec(query, map[string]interface{}{
		"expires_at": time.Now(),
		"id":         session.ID,
	})

	return err
}

// Delete deletes the specified session from the sessions table
func (s *SQLiteSession) Delete(db *sqlx.DB, table string, session *Session) error {
	if session.ID == "" {
		return ErrSessionInvalid
	}

	query := fmt.Sprintf("DELETE FROM %s WHERE id = ?", table)
	_, err := db.Exec(query, session.ID)

	return err
}
