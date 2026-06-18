package db

import "database/sql"

type WatcherStatus struct {
	Name             string
	LastSuccess      string
	LastError        string
	LastErrorMessage string
}

func (db *DB) GetWatcherStatus(name string) (*WatcherStatus, error) {
	var ws WatcherStatus
	var lastSuccess, lastError, lastErrorMsg sql.NullString
	err := db.conn.QueryRow(`
		SELECT name, last_success, last_error, last_error_message
		FROM watcher_status WHERE name = ?
	`, name).Scan(&ws.Name, &lastSuccess, &lastError, &lastErrorMsg)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if lastSuccess.Valid {
		ws.LastSuccess = lastSuccess.String
	}
	if lastError.Valid {
		ws.LastError = lastError.String
	}
	if lastErrorMsg.Valid {
		ws.LastErrorMessage = lastErrorMsg.String
	}
	return &ws, nil
}

func (db *DB) RecordWatcherSuccess(name string) error {
	_, err := db.conn.Exec(`
		INSERT INTO watcher_status (name, last_success)
		VALUES (?, datetime('now'))
		ON CONFLICT(name) DO UPDATE SET last_success = datetime('now')
	`, name)
	return err
}

func (db *DB) RecordWatcherError(name, message string) error {
	_, err := db.conn.Exec(`
		INSERT INTO watcher_status (name, last_error, last_error_message)
		VALUES (?, datetime('now'), ?)
		ON CONFLICT(name) DO UPDATE SET last_error = datetime('now'), last_error_message = ?
	`, name, message, message)
	return err
}

// HasWatcherError returns true if the watcher's last_error is more recent than last_success.
func (db *DB) HasWatcherError(name string) bool {
	ws, err := db.GetWatcherStatus(name)
	if err != nil || ws == nil {
		return false
	}
	if ws.LastError == "" {
		return false
	}
	if ws.LastSuccess == "" {
		return true
	}
	return ws.LastError > ws.LastSuccess
}
