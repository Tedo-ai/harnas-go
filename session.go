package harnas

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
)

type Session struct {
	ID       string
	Log      *Log
	Metadata map[string]any
	Hooks    *Hooks
}

func NewSession(id string, log *Log, metadata map[string]any) *Session {
	if log == nil {
		log = NewLog()
	}
	if metadata == nil {
		metadata = map[string]any{}
	}
	return &Session{
		ID:       id,
		Log:      log,
		Metadata: metadata,
		Hooks:    NewHooks(),
	}
}

func CreateSession(metadata map[string]any) *Session {
	return NewSession("ses_"+newID(), NewLog(), metadata)
}

func (s *Session) Fork(atSeq int) *Session {
	forkedLog := NewLog()
	for _, event := range s.Log.Events() {
		if event.Seq > atSeq {
			break
		}
		forkedLog.Restore(event)
	}
	metadata := map[string]any{}
	for key, value := range s.Metadata {
		metadata[key] = value
	}
	metadata["forked_from"] = s.ID
	metadata["forked_at_seq"] = float64(atSeq)
	return NewSession("ses_"+newID(), forkedLog, metadata)
}

func (s *Session) Save(path string) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(map[string]any{
		"__session__": true,
		"id":          s.ID,
		"metadata":    s.Metadata,
	}); err != nil {
		return err
	}
	for _, event := range s.Log.Events() {
		id := event.ID
		if id == "" {
			id = eventID(event.Seq, event.Payload)
		}
		if err := encoder.Encode(map[string]any{
			"seq":     event.Seq,
			"id":      id,
			"type":    event.Type,
			"payload": event.Payload,
		}); err != nil {
			return err
		}
	}
	return nil
}

func LoadSession(path string) (*Session, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := splitJSONLines(data)
	if len(lines) == 0 {
		return nil, fmt.Errorf("session file is empty")
	}
	var header struct {
		Session  bool           `json:"__session__"`
		ID       string         `json:"id"`
		Metadata map[string]any `json:"metadata"`
	}
	if err := json.Unmarshal(lines[0], &header); err != nil {
		return nil, err
	}
	if !header.Session {
		return nil, fmt.Errorf("missing session header")
	}

	log := NewLog()
	for _, line := range lines[1:] {
		var row struct {
			Seq     int            `json:"seq"`
			ID      string         `json:"id"`
			Type    EventType      `json:"type"`
			Payload map[string]any `json:"payload"`
		}
		if err := json.Unmarshal(line, &row); err != nil {
			return nil, err
		}
		log.Restore(Event{ID: row.ID, Seq: row.Seq, Type: row.Type, Payload: row.Payload})
	}
	return NewSession(header.ID, log, header.Metadata), nil
}

func splitJSONLines(data []byte) [][]byte {
	out := [][]byte{}
	start := 0
	for i, b := range data {
		if b == '\n' {
			if i > start {
				out = append(out, data[start:i])
			}
			start = i + 1
		}
	}
	if start < len(data) {
		out = append(out, data[start:])
	}
	return out
}

func newID() string {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		panic(err)
	}
	bytes[6] = (bytes[6] & 0x0f) | 0x40
	bytes[8] = (bytes[8] & 0x3f) | 0x80
	return fmt.Sprintf(
		"%x-%x-%x-%x-%x",
		bytes[0:4],
		bytes[4:6],
		bytes[6:8],
		bytes[8:10],
		bytes[10:16],
	)
}
