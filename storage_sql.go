package harnas

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

type SQLStorageDialect string

const (
	SQLStorageDialectSQLite   SQLStorageDialect = "sqlite"
	SQLStorageDialectPostgres SQLStorageDialect = "postgres"
)

type SQLStorageOptions struct {
	Dialect     SQLStorageDialect
	TablePrefix string
}

type SQLStorageAdapter struct {
	db        *sql.DB
	sessionID string
	dialect   SQLStorageDialect
	prefix    string
}

func NewSQLStorageAdapter(db *sql.DB, sessionID string, opts SQLStorageOptions) *SQLStorageAdapter {
	dialect := opts.Dialect
	if dialect == "" {
		dialect = SQLStorageDialectSQLite
	}
	return &SQLStorageAdapter{
		db:        db,
		sessionID: sessionID,
		dialect:   dialect,
		prefix:    opts.TablePrefix,
	}
}

func EnsureSQLStorageSchema(db *sql.DB, opts SQLStorageOptions) error {
	dialect := opts.Dialect
	if dialect == "" {
		dialect = SQLStorageDialectSQLite
	}
	prefix := opts.TablePrefix
	sessions := sqlStorageIdent(prefix + "harnas_sessions")
	events := sqlStorageIdent(prefix + "harnas_events")

	var statements []string
	switch dialect {
	case SQLStorageDialectSQLite:
		statements = []string{
			fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
				session_id TEXT PRIMARY KEY,
				header_json TEXT NOT NULL
			)`, sessions),
			fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
				session_id TEXT NOT NULL,
				seq INTEGER NOT NULL,
				id TEXT NOT NULL,
				timestamp TEXT NOT NULL,
				type TEXT NOT NULL,
				payload_json TEXT NOT NULL,
				content_hash TEXT,
				PRIMARY KEY (session_id, seq)
			)`, events),
		}
	case SQLStorageDialectPostgres:
		statements = []string{
			fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
				session_id TEXT PRIMARY KEY,
				header_json JSONB NOT NULL
			)`, sessions),
			fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
				session_id TEXT NOT NULL,
				seq INTEGER NOT NULL,
				id TEXT NOT NULL,
				timestamp TEXT NOT NULL,
				type TEXT NOT NULL,
				payload_json JSONB NOT NULL,
				content_hash TEXT,
				PRIMARY KEY (session_id, seq)
			)`, events),
		}
	default:
		return fmt.Errorf("unsupported SQL storage dialect %q", dialect)
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}

func (a *SQLStorageAdapter) LoadSession() (*SessionHeader, error) {
	if a.sessionID == "" {
		return nil, nil
	}
	var data string
	err := a.db.QueryRow(a.selectHeaderSQL(), a.sessionID).Scan(&data)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return decodeSessionHeader([]byte(data))
}

func (a *SQLStorageAdapter) SaveHeader(header SessionHeader) error {
	if header.ID == "" {
		return fmt.Errorf("session header id is required")
	}
	if a.sessionID == "" {
		a.sessionID = header.ID
	}
	if header.ID != a.sessionID {
		return fmt.Errorf("session header id %q does not match adapter session id %q", header.ID, a.sessionID)
	}
	data, err := json.Marshal(headerMap(header))
	if err != nil {
		return err
	}
	_, err = a.db.Exec(a.upsertHeaderSQL(), a.sessionID, string(data))
	return err
}

func (a *SQLStorageAdapter) AppendEvent(draft EventDraft, expectedNextSeq *int) (EventRow, error) {
	if a.sessionID == "" {
		return EventRow{}, fmt.Errorf("SQLStorageAdapter requires a session id")
	}
	tx, err := a.db.BeginTx(context.Background(), nil)
	if err != nil {
		return EventRow{}, err
	}
	defer tx.Rollback()

	nextSeq, err := a.currentNextSeqTx(tx)
	if err != nil {
		return EventRow{}, err
	}
	if expectedNextSeq != nil && *expectedNextSeq != nextSeq {
		return EventRow{}, &StorageConflictError{
			Reason:         StorageConflictReason,
			ExpectedSeq:    *expectedNextSeq,
			CurrentNextSeq: nextSeq,
		}
	}
	row := EventRow{
		Seq:       nextSeq,
		ID:        draft.ID,
		Timestamp: draft.Timestamp,
		Type:      draft.Type,
		Payload:   clonePayload(draft.Payload),
	}
	hash, err := ContentHashForEventRow(row)
	if err != nil {
		return EventRow{}, err
	}
	row.ContentHash = hash
	payloadJSON, err := jsonString(row.Payload)
	if err != nil {
		return EventRow{}, err
	}
	if _, err := tx.Exec(a.insertEventSQL(), a.sessionID, row.Seq, row.ID, row.Timestamp, string(row.Type), payloadJSON, row.ContentHash); err != nil {
		if expectedNextSeq != nil && a.isUniqueConflict(err) {
			current, currentErr := a.currentNextSeqTx(tx)
			if currentErr != nil {
				current = nextSeq
			}
			return EventRow{}, &StorageConflictError{
				Reason:         StorageConflictReason,
				ExpectedSeq:    *expectedNextSeq,
				CurrentNextSeq: current,
			}
		}
		return EventRow{}, err
	}
	if err := tx.Commit(); err != nil {
		return EventRow{}, err
	}
	return row, nil
}

func (a *SQLStorageAdapter) EventsSince(cursor *int) ([]EventRow, error) {
	if a.sessionID == "" {
		return []EventRow{}, nil
	}
	start := 0
	if cursor != nil {
		start = *cursor + 1
	}
	if start < 0 {
		start = 0
	}
	rows, err := a.db.Query(a.selectEventsSQL(), a.sessionID, start)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []EventRow{}
	expectedSeq := start
	for rows.Next() {
		row, err := scanSQLEventRow(rows)
		if err != nil {
			return nil, err
		}
		if row.Seq != expectedSeq {
			return nil, fmt.Errorf("invalid event seq at row %d: got %d, want %d", len(out), row.Seq, expectedSeq)
		}
		out = append(out, row)
		expectedSeq++
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (a *SQLStorageAdapter) currentNextSeqTx(tx *sql.Tx) (int, error) {
	var next sql.NullInt64
	if err := tx.QueryRow(a.currentNextSeqSQL(), a.sessionID).Scan(&next); err != nil {
		return 0, err
	}
	if !next.Valid {
		return 0, nil
	}
	return int(next.Int64), nil
}

func (a *SQLStorageAdapter) selectHeaderSQL() string {
	return fmt.Sprintf("SELECT header_json FROM %s WHERE session_id = %s",
		a.sessionsTable(), a.placeholder(1))
}

func (a *SQLStorageAdapter) upsertHeaderSQL() string {
	switch a.dialect {
	case SQLStorageDialectPostgres:
		return fmt.Sprintf(
			"INSERT INTO %s (session_id, header_json) VALUES (%s, %s) ON CONFLICT (session_id) DO UPDATE SET header_json = EXCLUDED.header_json",
			a.sessionsTable(), a.placeholder(1), a.placeholder(2),
		)
	default:
		return fmt.Sprintf(
			"INSERT INTO %s (session_id, header_json) VALUES (%s, %s) ON CONFLICT(session_id) DO UPDATE SET header_json = excluded.header_json",
			a.sessionsTable(), a.placeholder(1), a.placeholder(2),
		)
	}
}

func (a *SQLStorageAdapter) currentNextSeqSQL() string {
	return fmt.Sprintf("SELECT COALESCE(MAX(seq) + 1, 0) FROM %s WHERE session_id = %s",
		a.eventsTable(), a.placeholder(1))
}

func (a *SQLStorageAdapter) insertEventSQL() string {
	return fmt.Sprintf(
		"INSERT INTO %s (session_id, seq, id, timestamp, type, payload_json, content_hash) VALUES (%s, %s, %s, %s, %s, %s, %s)",
		a.eventsTable(),
		a.placeholder(1),
		a.placeholder(2),
		a.placeholder(3),
		a.placeholder(4),
		a.placeholder(5),
		a.placeholder(6),
		a.placeholder(7),
	)
}

func (a *SQLStorageAdapter) selectEventsSQL() string {
	return fmt.Sprintf(
		"SELECT seq, id, timestamp, type, payload_json, content_hash FROM %s WHERE session_id = %s AND seq >= %s ORDER BY seq ASC",
		a.eventsTable(), a.placeholder(1), a.placeholder(2),
	)
}

func (a *SQLStorageAdapter) sessionsTable() string {
	return sqlStorageIdent(a.prefix + "harnas_sessions")
}

func (a *SQLStorageAdapter) eventsTable() string {
	return sqlStorageIdent(a.prefix + "harnas_events")
}

func (a *SQLStorageAdapter) placeholder(index int) string {
	if a.dialect == SQLStorageDialectPostgres {
		return fmt.Sprintf("$%d", index)
	}
	return "?"
}

func (a *SQLStorageAdapter) isUniqueConflict(err error) bool {
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "unique") || strings.Contains(text, "duplicate")
}

func scanSQLEventRow(scanner interface {
	Scan(dest ...any) error
}) (EventRow, error) {
	var row EventRow
	var eventType string
	var payloadJSON string
	var contentHash sql.NullString
	if err := scanner.Scan(&row.Seq, &row.ID, &row.Timestamp, &eventType, &payloadJSON, &contentHash); err != nil {
		return EventRow{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader([]byte(payloadJSON)))
	decoder.UseNumber()
	if err := decoder.Decode(&row.Payload); err != nil {
		return EventRow{}, err
	}
	row.Type = EventType(eventType)
	if contentHash.Valid {
		row.ContentHash = contentHash.String
	}
	return row, nil
}

func jsonString(value any) (string, error) {
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return "", err
	}
	return strings.TrimSuffix(out.String(), "\n"), nil
}

func sqlStorageIdent(value string) string {
	if value == "" {
		value = "harnas"
	}
	escaped := strings.ReplaceAll(value, `"`, `""`)
	return `"` + escaped + `"`
}
