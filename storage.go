package harnas

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const StorageConflictReason = "storage_conflict"

type SessionHeader struct {
	ID               string
	Metadata         map[string]any
	ParentSessionID  string
	RootSessionID    string
	SpawnID          string
	SpawnedByEventID string
	DelegationChain  []map[string]any
}

type EventDraft struct {
	ID        string
	Timestamp string
	Type      EventType
	Payload   map[string]any
}

type EventRow struct {
	Seq         int
	ID          string
	Timestamp   string
	Type        EventType
	Payload     map[string]any
	ContentHash string
}

type StorageAdapter interface {
	LoadSession() (*SessionHeader, error)
	SaveHeader(SessionHeader) error
	AppendEvent(EventDraft, *int) (EventRow, error)
	AppendEvents([]EventDraft, *int) ([]EventRow, error)
	EventsSince(*int) ([]EventRow, error)
}

type StorageConflictError struct {
	Reason         string
	ExpectedSeq    int
	CurrentNextSeq int
}

func (e *StorageConflictError) Error() string {
	return fmt.Sprintf("%s: expected next seq %d, current next seq %d", e.Reason, e.ExpectedSeq, e.CurrentNextSeq)
}

type MemoryStorageAdapter struct {
	header *SessionHeader
	rows   []EventRow
}

func NewMemoryStorageAdapter() *MemoryStorageAdapter {
	return &MemoryStorageAdapter{rows: []EventRow{}}
}

func (a *MemoryStorageAdapter) LoadSession() (*SessionHeader, error) {
	if a.header == nil {
		return nil, nil
	}
	header := cloneSessionHeader(*a.header)
	return &header, nil
}

func (a *MemoryStorageAdapter) SaveHeader(header SessionHeader) error {
	cloned := cloneSessionHeader(header)
	a.header = &cloned
	return nil
}

func (a *MemoryStorageAdapter) AppendEvent(draft EventDraft, expectedNextSeq *int) (EventRow, error) {
	rows, err := a.AppendEvents([]EventDraft{draft}, expectedNextSeq)
	if err != nil {
		return EventRow{}, err
	}
	return rows[0], nil
}

func (a *MemoryStorageAdapter) AppendEvents(drafts []EventDraft, expectedNextSeq *int) ([]EventRow, error) {
	nextSeq := len(a.rows)
	if expectedNextSeq != nil && *expectedNextSeq != nextSeq {
		return nil, &StorageConflictError{Reason: StorageConflictReason, ExpectedSeq: *expectedNextSeq, CurrentNextSeq: nextSeq}
	}
	rows := make([]EventRow, 0, len(drafts))
	for index, draft := range drafts {
		rows = append(rows, EventRow{
			Seq:       nextSeq + index,
			ID:        draft.ID,
			Timestamp: draft.Timestamp,
			Type:      draft.Type,
			Payload:   clonePayload(draft.Payload),
		})
	}
	a.rows = append(a.rows, rows...)
	return rows, nil
}

func (a *MemoryStorageAdapter) EventsSince(cursor *int) ([]EventRow, error) {
	start := 0
	if cursor != nil {
		start = *cursor + 1
	}
	if start < 0 {
		start = 0
	}
	if start > len(a.rows) {
		return []EventRow{}, nil
	}
	out := make([]EventRow, 0, len(a.rows)-start)
	for _, row := range a.rows[start:] {
		out = append(out, cloneEventRow(row))
	}
	return out, nil
}

type FileStorageAdapter struct {
	path string
}

func NewFileStorageAdapter(path string) *FileStorageAdapter {
	return &FileStorageAdapter{path: path}
}

func (a *FileStorageAdapter) LoadSession() (*SessionHeader, error) {
	header, _, err := a.readAll()
	if os.IsNotExist(err) {
		return nil, nil
	}
	return header, err
}

func (a *FileStorageAdapter) SaveHeader(header SessionHeader) error {
	_, rows, err := a.readAll()
	if os.IsNotExist(err) {
		rows = []EventRow{}
	} else if err != nil {
		return err
	}
	return a.writeAll(&header, rows)
}

func (a *FileStorageAdapter) AppendEvent(draft EventDraft, expectedNextSeq *int) (EventRow, error) {
	rows, err := a.AppendEvents([]EventDraft{draft}, expectedNextSeq)
	if err != nil {
		return EventRow{}, err
	}
	return rows[0], nil
}

func (a *FileStorageAdapter) AppendEvents(drafts []EventDraft, expectedNextSeq *int) ([]EventRow, error) {
	header, rows, err := a.readAll()
	if os.IsNotExist(err) {
		header = nil
		rows = []EventRow{}
	} else if err != nil {
		return nil, err
	}
	nextSeq := len(rows)
	if expectedNextSeq != nil && *expectedNextSeq != nextSeq {
		return nil, &StorageConflictError{Reason: StorageConflictReason, ExpectedSeq: *expectedNextSeq, CurrentNextSeq: nextSeq}
	}
	appended := make([]EventRow, 0, len(drafts))
	for index, draft := range drafts {
		appended = append(appended, EventRow{
			Seq:       nextSeq + index,
			ID:        draft.ID,
			Timestamp: draft.Timestamp,
			Type:      draft.Type,
			Payload:   clonePayload(draft.Payload),
		})
	}
	rows = append(rows, appended...)
	if err := a.writeAll(header, rows); err != nil {
		return nil, err
	}
	return appended, nil
}

func (a *FileStorageAdapter) EventsSince(cursor *int) ([]EventRow, error) {
	_, rows, err := a.readAll()
	if os.IsNotExist(err) {
		return []EventRow{}, nil
	}
	if err != nil {
		return nil, err
	}
	start := 0
	if cursor != nil {
		start = *cursor + 1
	}
	if start < 0 {
		start = 0
	}
	if start > len(rows) {
		return []EventRow{}, nil
	}
	out := make([]EventRow, 0, len(rows)-start)
	for _, row := range rows[start:] {
		out = append(out, cloneEventRow(row))
	}
	return out, nil
}

func (a *FileStorageAdapter) readAll() (*SessionHeader, []EventRow, error) {
	file, err := os.Open(a.path)
	if err != nil {
		return nil, nil, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	var header *SessionHeader
	rows := []EventRow{}
	lineNo := 0
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		if lineNo == 0 {
			parsed, err := decodeSessionHeader(line)
			if err != nil {
				return nil, nil, err
			}
			header = parsed
		} else {
			row, err := decodeEventRow(line)
			if err != nil {
				return nil, nil, err
			}
			expectedSeq := len(rows)
			if row.Seq != expectedSeq {
				return nil, nil, fmt.Errorf("invalid event seq at row %d: got %d, want %d", lineNo, row.Seq, expectedSeq)
			}
			rows = append(rows, row)
		}
		lineNo++
	}
	if err := scanner.Err(); err != nil {
		return nil, nil, err
	}
	if header == nil {
		return nil, nil, fmt.Errorf("session file is empty")
	}
	return header, rows, nil
}

func (a *FileStorageAdapter) writeAll(header *SessionHeader, rows []EventRow) error {
	dir := filepath.Dir(a.path)
	file, err := os.CreateTemp(dir, ".harnas-storage-*")
	if err != nil {
		return err
	}
	tempPath := file.Name()
	committed := false
	defer func() {
		_ = file.Close()
		if !committed {
			_ = os.Remove(tempPath)
		}
	}()
	mode := os.FileMode(0o644)
	if info, statErr := os.Stat(a.path); statErr == nil {
		mode = info.Mode().Perm()
	}
	if err := file.Chmod(mode); err != nil {
		return err
	}
	encoder := json.NewEncoder(file)
	encoder.SetEscapeHTML(false)
	if header != nil {
		if err := encoder.Encode(headerMap(*header)); err != nil {
			return err
		}
	}
	for _, row := range rows {
		if err := encoder.Encode(eventRowMap(row, row.ContentHash != "")); err != nil {
			return err
		}
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, a.path); err != nil {
		return err
	}
	committed = true
	return nil
}

func decodeSessionHeader(data []byte) (*SessionHeader, error) {
	var raw struct {
		Session          bool             `json:"__session__"`
		ID               string           `json:"id"`
		Metadata         map[string]any   `json:"metadata"`
		ParentSessionID  string           `json:"parent_session_id"`
		RootSessionID    string           `json:"root_session_id"`
		SpawnID          string           `json:"spawn_id"`
		SpawnedByEventID string           `json:"spawned_by_event_id"`
		DelegationChain  []map[string]any `json:"delegation_chain"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	if !raw.Session {
		return nil, fmt.Errorf("missing session header")
	}
	if raw.Metadata == nil {
		raw.Metadata = map[string]any{}
	}
	return &SessionHeader{
		ID:               raw.ID,
		Metadata:         raw.Metadata,
		ParentSessionID:  raw.ParentSessionID,
		RootSessionID:    raw.RootSessionID,
		SpawnID:          raw.SpawnID,
		SpawnedByEventID: raw.SpawnedByEventID,
		DelegationChain:  raw.DelegationChain,
	}, nil
}

func decodeEventRow(data []byte) (EventRow, error) {
	var row struct {
		Seq         int            `json:"seq"`
		ID          string         `json:"id"`
		Timestamp   string         `json:"timestamp"`
		Type        EventType      `json:"type"`
		Payload     map[string]any `json:"payload"`
		ContentHash string         `json:"content_hash"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&row); err != nil {
		return EventRow{}, err
	}
	return EventRow{Seq: row.Seq, ID: row.ID, Timestamp: row.Timestamp, Type: row.Type, Payload: row.Payload, ContentHash: row.ContentHash}, nil
}

func headerMap(header SessionHeader) map[string]any {
	out := map[string]any{
		"__session__": true,
		"id":          header.ID,
		"metadata":    header.Metadata,
	}
	if header.ParentSessionID != "" {
		out["parent_session_id"] = header.ParentSessionID
	}
	if header.RootSessionID != "" {
		out["root_session_id"] = header.RootSessionID
	}
	if header.SpawnID != "" {
		out["spawn_id"] = header.SpawnID
	}
	if header.SpawnedByEventID != "" {
		out["spawned_by_event_id"] = header.SpawnedByEventID
	}
	if len(header.DelegationChain) > 0 {
		out["delegation_chain"] = header.DelegationChain
	}
	return out
}

func eventRowMap(row EventRow, includeContentHash bool) map[string]any {
	out := map[string]any{
		"seq":       row.Seq,
		"id":        row.ID,
		"timestamp": row.Timestamp,
		"type":      string(row.Type),
		"payload":   row.Payload,
	}
	if includeContentHash {
		out["content_hash"] = row.ContentHash
	}
	return out
}

func cloneSessionHeader(header SessionHeader) SessionHeader {
	return SessionHeader{
		ID:               header.ID,
		Metadata:         clonePayload(header.Metadata),
		ParentSessionID:  header.ParentSessionID,
		RootSessionID:    header.RootSessionID,
		SpawnID:          header.SpawnID,
		SpawnedByEventID: header.SpawnedByEventID,
		DelegationChain:  cloneDelegationChain(header.DelegationChain),
	}
}

func cloneEventRow(row EventRow) EventRow {
	return EventRow{
		Seq:         row.Seq,
		ID:          row.ID,
		Timestamp:   row.Timestamp,
		Type:        row.Type,
		Payload:     clonePayload(row.Payload),
		ContentHash: row.ContentHash,
	}
}

func clonePayload(payload map[string]any) map[string]any {
	if payload == nil {
		return map[string]any{}
	}
	out := map[string]any{}
	for key, value := range payload {
		out[key] = cloneJSONValue(value)
	}
	return out
}

func cloneJSONValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		return clonePayload(v)
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = cloneJSONValue(item)
		}
		return out
	case []map[string]any:
		out := make([]map[string]any, len(v))
		for i, item := range v {
			out[i] = clonePayload(item)
		}
		return out
	default:
		return v
	}
}
