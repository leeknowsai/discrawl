package auth

import (
	"database/sql"
	"time"
)

// SQLiteStore implements scs.Store backed by the meta.db sessions table.
type SQLiteStore struct {
	db          *sql.DB
	stopCleanup chan struct{}
}

// NewSQLiteStore creates a new SQLiteStore and starts a background cleanup goroutine.
func NewSQLiteStore(db *sql.DB, cleanupInterval time.Duration) *SQLiteStore {
	s := &SQLiteStore{
		db:          db,
		stopCleanup: make(chan struct{}),
	}
	if cleanupInterval > 0 {
		go s.cleanup(cleanupInterval)
	}
	return s
}

// Find returns the data for a session token.
func (s *SQLiteStore) Find(token string) ([]byte, bool, error) {
	var data []byte
	var expiry float64
	err := s.db.QueryRow(
		`select data, expiry from sessions where token = ?`, token,
	).Scan(&data, &expiry)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	// expiry stored as Unix seconds (float64)
	if float64(time.Now().Unix()) > expiry {
		return nil, false, nil
	}
	return data, true, nil
}

// Commit saves session data with the given expiry.
func (s *SQLiteStore) Commit(token string, b []byte, expiry time.Time) error {
	_, err := s.db.Exec(
		`insert into sessions(token, data, expiry) values(?, ?, ?)
		 on conflict(token) do update set data=excluded.data, expiry=excluded.expiry`,
		token, b, float64(expiry.Unix()),
	)
	return err
}

// Delete removes a session token.
func (s *SQLiteStore) Delete(token string) error {
	_, err := s.db.Exec(`delete from sessions where token = ?`, token)
	return err
}

// StopCleanup stops the background cleanup goroutine.
func (s *SQLiteStore) StopCleanup() {
	close(s.stopCleanup)
}

func (s *SQLiteStore) cleanup(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			_, _ = s.db.Exec(`delete from sessions where expiry < ?`, float64(time.Now().Unix()))
		case <-s.stopCleanup:
			return
		}
	}
}
